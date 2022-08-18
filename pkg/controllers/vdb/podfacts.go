/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package vdb

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"yunion.io/x/pkg/tristate"
)

// PodFact keeps track of facts for a specific pod
type PodFact struct {
	// Name of the pod
	name types.NamespacedName

	// Index of the pod within the subcluster.  0 means it is the first pod.
	podIndex int32

	// dns name resolution of the pod
	dnsName string

	// IP address of the pod
	podIP string

	// Name of the subcluster the pod is part of
	subcluster string

	// true if this node is part of a primary subcluster
	isPrimary bool

	// The image that is currently running in the pod
	image string

	// true means the pod exists in k8s.  false means it hasn't been created yet.
	exists bool

	// true means the pod has been bound to a node, and all of the containers
	// have been created. At least one container is still running, or is in the
	// process of starting or restarting.
	isPodRunning bool

	// true means the statefulset exists and its size includes this pod.  The
	// cases where this would be false are (a) statefulset doesn't yet exist or
	// (b) statefulset exists but it isn't sized to include this pod yet.
	managedByParent bool

	// true means the pod is scheduled for deletion.  This can happen if the
	// size of the subcluster has shrunk in the VerticaDB but the pod still
	// exists and is managed by a statefulset.  The pod is pending delete in
	// that once the statefulset is sized according to the subcluster the pod
	// will get deleted.
	pendingDelete bool

	// Have we run install for this pod?
	isInstalled bool

	// Does admintools.conf exist but is for an old vdb?
	hasStaleAdmintoolsConf bool

	// Does the database exist at this pod? This is true iff the database was
	// created and this pod has been added to the vertica cluster.
	dbExists bool

	// true means the pod has a running vertica process accepting connections on
	// port 5433.
	upNode bool

	// true means the node is up, but in read-only state
	readOnly bool

	// The vnode name that Vertica assigned to this pod.
	vnodeName string

	// The compat21 node name that Vertica assignes to the pod. This is only set
	// if installation has occurred and the initPolicy is not ScheduleOnly.
	compat21NodeName string

	// True if the end user license agreement has been accepted
	eulaAccepted tristate.TriState

	// True if /opt/vertica/config/logrotate exists
	configLogrotateExists bool

	// True if /opt/vertica/config/logrotate is writable by dbadmin
	configLogrotateWritable bool

	// True if /opt/vertica/config/share exists
	configShareExists bool

	// True if /opt/vertica/config/https_certs/httpstls.json file exists
	httpTLSConfExists bool

	// True if this pod is for a transient subcluster created for online upgrade
	isTransient bool

	// The number of shards this node has subscribed to, not including the
	// special replica shard that has unsegmented projections.
	shardSubscriptions int

	// We add annotations to the pod for the k8s DC table.  This is an
	// indication that the pod already has them.
	hasDCTableAnnotations bool

	// If the depot is sized to be a % of the local disk, this is the
	// percentage.  If depot is a fixed sized, then this is empty.  This is only
	// valid if the database is up.
	depotDiskPercentSize string

	// The size of the depot in bytes.  This is only valid if the database is up.
	maxDepotSize int
}

type PodFactDetail map[types.NamespacedName]*PodFact

// A collection of facts for many pods.
type PodFacts struct {
	VRec           *VerticaDBReconciler
	PRunner        cmds.PodRunner
	Detail         PodFactDetail
	NeedCollection bool
}

type CheckType string

const (
	CheckDirExists  CheckType = "-d"
	CheckFileExists CheckType = "-f"
	CheckWritable   CheckType = "-w"
)

// MakePodFacts will create a PodFacts object and return it
func MakePodFacts(vrec *VerticaDBReconciler, prunner cmds.PodRunner) PodFacts {
	return PodFacts{VRec: vrec, PRunner: prunner, NeedCollection: true, Detail: make(PodFactDetail)}
}

// Collect will gather up the for facts if a collection is needed
// If the facts are already up to date, this function does nothing.
func (p *PodFacts) Collect(ctx context.Context, vdb *vapi.VerticaDB) error {
	// Skip if already up to date
	if !p.NeedCollection {
		return nil
	}
	p.Detail = make(PodFactDetail) // Clear as there may be some items cached

	// Find all of the subclusters to collect facts for.  We want to include all
	// subclusters, even ones that are scheduled to be deleted -- we keep
	// collecting facts for those until the statefulsets are gone.
	finder := iter.MakeSubclusterFinder(p.VRec.Client, vdb)
	subclusters, err := finder.FindSubclusters(ctx, iter.FindAll)
	if err != nil {
		return nil
	}

	// Collect all of the facts about each running pod
	for i := range subclusters {
		if err := p.collectSubcluster(ctx, vdb, subclusters[i]); err != nil {
			return err
		}
	}
	p.NeedCollection = false
	return nil
}

// Invalidate will mark the pod facts as requiring a refresh.
// Next call to Collect will gather up the facts again.
func (p *PodFacts) Invalidate() {
	p.NeedCollection = true
}

// collectSubcluster will collect facts about each pod in a specific subcluster
func (p *PodFacts) collectSubcluster(ctx context.Context, vdb *vapi.VerticaDB, sc *vapi.Subcluster) error {
	sts := &appsv1.StatefulSet{}
	maxStsSize := sc.Size
	// Attempt to fetch the sts.  We continue even for 'not found' errors
	// because we want to populate the missing pods into the pod facts.
	if err := p.VRec.Client.Get(ctx, names.GenStsName(vdb, sc), sts); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("could not fetch statefulset for pod fact collection %s %w", sc.Name, err)
	} else if sts.Spec.Replicas != nil && *sts.Spec.Replicas > maxStsSize {
		maxStsSize = *sts.Spec.Replicas
	}

	for i := int32(0); i < maxStsSize; i++ {
		if err := p.collectPodByStsIndex(ctx, vdb, sc, sts, i); err != nil {
			return err
		}
	}
	return nil
}

// collectPodByStsIndex will collect facts about a single pod in a subcluster
func (p *PodFacts) collectPodByStsIndex(ctx context.Context, vdb *vapi.VerticaDB, sc *vapi.Subcluster,
	sts *appsv1.StatefulSet, podIndex int32) error {
	pf := PodFact{
		name:       names.GenPodName(vdb, sc, podIndex),
		subcluster: sc.Name,
		isPrimary:  sc.IsPrimary,
		podIndex:   podIndex,
	}
	// It is possible for a pod to be managed by a parent sts but not yet exist.
	// So, this has to be checked before we check for pod existence.
	if sts.Spec.Replicas != nil {
		pf.managedByParent = podIndex < *sts.Spec.Replicas
	}

	pod := &corev1.Pod{}
	if err := p.VRec.Client.Get(ctx, pf.name, pod); err != nil && !errors.IsNotFound(err) {
		return err
	} else if err == nil {
		// Treat not found errors as if the pod is not running.  We continue
		// checking other elements.  There are certain states, such as
		// isInstalled or dbExists, that can be determined when the pod isn't
		// running.
		//
		// The remaining fields we set in this block only make sense when the
		// pod exists.
		pf.exists = true // Success from the Get() implies pod exists in API server
		pf.isPodRunning = pod.Status.Phase == corev1.PodRunning
		pf.dnsName = pod.Spec.Hostname + "." + pod.Spec.Subdomain
		pf.podIP = pod.Status.PodIP
		pf.isTransient, _ = strconv.ParseBool(pod.Labels[builder.SubclusterTransientLabel])
		pf.pendingDelete = podIndex >= sc.Size
		pf.image = pod.Spec.Containers[ServerContainerIndex].Image
		pf.hasDCTableAnnotations = p.checkDCTableAnnotations(pod)
	}

	fns := []func(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error{
		p.checkIsInstalled,
		p.checkIsDBCreated,
		p.checkIfNodeIsUpAndReadOnly,
		p.checkEulaAcceptance,
		p.checkLogrotateExists,
		p.checkIsLogrotateWritable,
		p.checkThatConfigShareExists,
		p.checkThatHTTPTLSConfExists,
		p.checkShardSubscriptions,
		p.queryDepotDetails,
	}

	for _, fn := range fns {
		if err := fn(ctx, vdb, &pf); err != nil {
			return err
		}
	}

	p.Detail[pf.name] = &pf
	return nil
}

// checkIsInstalled will check a single pod to see if the installation has happened.
func (p *PodFacts) checkIsInstalled(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	pf.isInstalled = false

	scs, ok := vdb.FindSubclusterStatus(pf.subcluster)
	if ok {
		// Set the install indicator first based on the install count in the status
		// field.  There are a couple of cases where this will give us the wrong state:
		// 1.  We have done the install, but haven't yet updated the status field.
		// 2.  We have done the install, but the admintools.conf was deleted after the fact.
		// So, we continue after this to further refine the actual install state.
		pf.isInstalled = scs.InstallCount > pf.podIndex
	}
	// Nothing else can be gathered if the pod isn't running.
	if !pf.isPodRunning {
		return nil
	}

	// If initPolicy is ScheduleOnly, there is no install indicator since the
	// operator didn't initiate it.  We are going to do based on the existence
	// of admintools.conf.
	if vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		if !pf.isInstalled {
			if _, _, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "test", "-f", paths.AdminToolsConf); err == nil {
				pf.isInstalled = true
			}
		}

		// We can't reliably set compat21NodeName because the operator didn't
		// originate the install.  We will intentionally leave that blank.
		pf.compat21NodeName = ""

		return nil
	}

	fn := vdb.GenInstallerIndicatorFileName()
	if stdout, stderr, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "cat", fn); err != nil {
		if !strings.Contains(stderr, "cat: "+fn+": No such file or directory") {
			return err
		}
		pf.isInstalled = false

		// Check if there is a stale admintools.conf
		cmd := []string{"ls", paths.AdminToolsConf}
		if _, stderr, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, cmd...); err != nil {
			if !strings.Contains(stderr, "No such file or directory") {
				return err
			}
			pf.hasStaleAdmintoolsConf = false
		} else {
			pf.hasStaleAdmintoolsConf = true
		}
	} else {
		pf.isInstalled = true
		pf.compat21NodeName = strings.TrimSuffix(stdout, "\n")
	}
	return nil
}

// checkEulaAcceptance will check if the end user license agreement has been accepted
func (p *PodFacts) checkEulaAcceptance(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	if pf.isPodRunning {
		if _, stderr, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "cat", paths.EulaAcceptanceFile); err != nil {
			if !strings.Contains(stderr, fmt.Sprintf("cat: %s: No such file or directory", paths.EulaAcceptanceFile)) {
				return err
			}
			pf.eulaAccepted = tristate.False
		} else {
			pf.eulaAccepted = tristate.True
		}
	}
	return nil
}

// checkLogrotateExists will verify that that /opt/vertica/config/logrotate exists
func (p *PodFacts) checkLogrotateExists(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	return p.checkDir(ctx, pf, CheckDirExists, paths.ConfigLogrotatePath, func() { pf.configLogrotateExists = true })
}

// checkIsLogrotateWritable will verify that dbadmin has write access to /opt/vertica/config/logrotate
func (p *PodFacts) checkIsLogrotateWritable(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	return p.checkDir(ctx, pf, CheckWritable, paths.ConfigLogrotatePath, func() { pf.configLogrotateWritable = true })
}

// checkThatConfigShareExists will verify that /opt/vertica/config/share exists
func (p *PodFacts) checkThatConfigShareExists(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	return p.checkDir(ctx, pf, CheckDirExists, paths.ConfigSharePath, func() { pf.configShareExists = true })
}

// checkThatHTTPTLSConfExists will verify that http service config file exists
func (p *PodFacts) checkThatHTTPTLSConfExists(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	return p.checkDir(ctx, pf, CheckFileExists, paths.HTTPTLSConfPath, func() { pf.httpTLSConfExists = true })
}

// checkDir is a general function that will check if a directory (exists,
// writable, etc.) and callback to a function when it passes the check.
func (p *PodFacts) checkDir(ctx context.Context, pf *PodFact, check CheckType, dir string, onSuccessCallback func()) error {
	if pf.isPodRunning {
		if _, _, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "test", string(check), dir); err == nil {
			onSuccessCallback()
		}
	}
	return nil
}

// checkShardSubscriptions will count the number of shards that are subscribed
// to the current node
func (p *PodFacts) checkShardSubscriptions(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	// This check depends on the vnode, which is only present if the pod is
	// running and the database exists at the node.
	if !pf.isPodRunning || !pf.dbExists {
		return nil
	}
	cmd := []string{
		"-tAc",
		fmt.Sprintf("select count(*) from v_catalog.node_subscriptions where node_name = '%s' and shard_name != 'replica'",
			pf.vnodeName),
	}
	stdout, _, err := p.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	if err != nil {
		// An error implies the server is down, so skipping this check.
		return nil
	}
	return setShardSubscription(stdout, pf)
}

// queryDepotDetails will query the database to get info about the depot for the node
func (p *PodFacts) queryDepotDetails(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	// This check depends on the database being up
	if !pf.isPodRunning || !pf.upNode {
		return nil
	}
	cmd := []string{
		"-tAc",
		fmt.Sprintf("select max_size, disk_percent from storage_locations "+
			"where location_usage = 'DEPOT' and node_name = '%s'", pf.vnodeName),
	}
	stdout, _, err := p.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	if err != nil {
		// An error implies the server is down, so skipping this check.
		return nil
	}
	return pf.setDepotDetails(stdout)
}

// setDepotDetails will set depot details in the PodFacts based on the query output
func (p *PodFact) setDepotDetails(op string) error {
	// For testing purposes, return without error if there is no output
	if op == "" {
		return nil
	}
	lines := strings.Split(op, "\n")
	cols := strings.Split(lines[0], "|")
	const ExpectedCols = 2
	if len(cols) != ExpectedCols {
		return fmt.Errorf("expected %d columns from storage_locations query but only got %d", ExpectedCols, len(cols))
	}
	var err error
	p.maxDepotSize, err = strconv.Atoi(cols[0])
	if err != nil {
		return err
	}
	p.depotDiskPercentSize = cols[1]
	return nil
}

// checkDCTableAnnotations will check if the pod has the necessary annotations
// to populate the DC tables that we log at vertica start.
func (p *PodFacts) checkDCTableAnnotations(pod *corev1.Pod) bool {
	// We just look for one annotation.  This works because they are always added together.
	_, ok := pod.Annotations[builder.KubernetesVersionAnnotation]
	return ok
}

// checkIsDBCreated will check for evidence of a database at the local node.
// If a db is found, we will set the vertica node name.
func (p *PodFacts) checkIsDBCreated(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	pf.dbExists = false

	scs, ok := vdb.FindSubclusterStatus(pf.subcluster)
	if ok {
		// Set the db exists indicator first based on the count in the status
		// field.  We continue to check the path as we do that to figure out the
		// vnode.
		pf.dbExists = scs.AddedToDBCount > pf.podIndex
		// Inherit the vnode name if present
		if int(pf.podIndex) < len(scs.Detail) {
			pf.vnodeName = scs.Detail[pf.podIndex].VNodeName
		}
	}
	// Nothing else can be gathered if the pod isn't running.
	if !pf.isPodRunning {
		return nil
	}

	cmd := []string{
		"bash",
		"-c",
		fmt.Sprintf("ls -d %s/v_%s_node????_data", vdb.GetDBDataPath(), strings.ToLower(vdb.Spec.DBName)),
	}
	if stdout, stderr, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, cmd...); err != nil {
		if !strings.Contains(stderr, "No such file or directory") {
			return err
		}
		pf.dbExists = false
	} else {
		pf.dbExists = true
		pf.vnodeName = parseVerticaNodeName(stdout)
	}

	return nil
}

// checkIfNodeIsUpAndReadOnly will determine whether Vertica process is running
// in the pod and whether it is in read-only mode.
func (p *PodFacts) checkIfNodeIsUpAndReadOnly(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	if !pf.dbExists || !pf.isPodRunning {
		pf.upNode = false
		pf.readOnly = false
		return nil
	}

	// The read-only state is a new state added in 11.0.2.  So we can only query
	// for it on levels 11.0.2+.  Otherwise, we always treat read-only as being
	// disabled.
	vinf, ok := version.MakeInfoFromVdb(vdb)
	if ok && vinf.IsEqualOrNewer(version.NodesHaveReadOnlyStateVersion) {
		return p.queryNodeStatus(ctx, pf)
	}
	return p.checkIfNodeIsUp(ctx, pf)
}

// checkIfNodeIsUp will check if the Vertica is up and running in this process.
// It assumes the pod is running and the database exists.  It doesn't check for
// read-only state.  This exists for backwards compatibility of versions older
// than 11.0.2.
func (p *PodFacts) checkIfNodeIsUp(ctx context.Context, pf *PodFact) error {
	cmd := []string{
		"-c",
		"select 1",
	}
	if _, stderr, err := p.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...); err != nil {
		if !strings.Contains(stderr, "vsql: could not connect to server:") {
			return err
		}
		pf.upNode = false
	} else {
		pf.upNode = true
	}
	// This is called for server versions that don't have read-only state.  So
	// read-only will always be false.
	pf.readOnly = false

	return nil
}

// queryNodeStatus will query the nodes system table to see if the node is up
// and wether it is in read-only state.  It assumes the database exists and the
// pod is running.
func (p *PodFacts) queryNodeStatus(ctx context.Context, pf *PodFact) error {
	cmd := []string{
		"-tAc",
		"select node_state, is_readonly " +
			"from nodes " +
			"where node_name in (select node_name from current_session)",
	}
	if stdout, _, err := p.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...); err != nil {
		pf.upNode = false
		pf.readOnly = false
	} else if pf.upNode, pf.readOnly, err = parseNodeStateAndReadOnly(stdout); err != nil {
		return err
	}

	return nil
}

// parseNodeStateAndReadOnly will parse query output to determine if a node is
// up and read-only.
func parseNodeStateAndReadOnly(stdout string) (upNode, readOnly bool, err error) {
	// For testing purposes we early out with no error if there is no output
	if stdout == "" {
		return
	}
	// The stdout comes in the form like this:
	// UP|t
	// This means upNode is true and readOnly is true.
	lines := strings.Split(stdout, "\n")
	cols := strings.Split(lines[0], "|")
	const ExpectedCols = 2
	if len(cols) != ExpectedCols {
		err = fmt.Errorf("expected %d columns from node query but only got %d", ExpectedCols, len(cols))
		return
	}
	upNode = cols[0] == "UP"
	readOnly = cols[1] == "t"
	return
}

// parseVerticaNodeName extract the vertica node name from the directory list
func parseVerticaNodeName(stdout string) string {
	re := regexp.MustCompile(`(v_.+_node\d+)_data`)
	match := re.FindAllStringSubmatch(stdout, 1)
	if len(match) > 0 && len(match[0]) > 0 {
		return match[0][1]
	}
	return ""
}

// setShardSubscription will set the pf.shardSubscriptions based on the query
// output
func setShardSubscription(op string, pf *PodFact) error {
	// For testing purposes we early out with no error if there is no output
	if op == "" {
		return nil
	}

	lines := strings.Split(op, "\n")
	subs, err := strconv.Atoi(lines[0])
	if err != nil {
		return err
	}
	pf.shardSubscriptions = subs
	return nil
}

// doesDBExist will check if the database exists anywhere.
// Returns false if we are 100% confident that the database doesn't
// exist anywhere.
func (p *PodFacts) doesDBExist() bool {
	for _, v := range p.Detail {
		if v.dbExists {
			return true
		}
	}
	return false
}

// findPodsWithMisstingDB will return a list of pods facts that have a missing DB.
// It will only return pods that are running and that match the given
// subcluster. If no pods are found an empty list is returned. The list will be
// ordered by pod index.  We also return a bool indicating if at least one pod
// that has a missing DB wasn't running.
func (p *PodFacts) findPodsWithMissingDB(scName string) ([]*PodFact, bool) {
	podsHaveMissingDBAndNotRunning := false
	hostList := []*PodFact{}
	for _, v := range p.Detail {
		if v.subcluster != scName {
			continue
		}
		if !v.dbExists {
			if !v.isPodRunning {
				podsHaveMissingDBAndNotRunning = true
			}
			hostList = append(hostList, v)
		}
	}
	// Return an ordered list by pod index for easier debugging
	sort.Slice(hostList, func(i, j int) bool {
		return hostList[i].dnsName < hostList[j].dnsName
	})
	return hostList, podsHaveMissingDBAndNotRunning
}

// findPodToRunVsql returns the name of the pod we will exec into in
// order to run vsql
// Will return false for second parameter if no pod could be found.
func (p *PodFacts) findPodToRunVsql(allowReadOnly bool, scName string) (*PodFact, bool) {
	for _, v := range p.Detail {
		if scName != "" && v.subcluster != scName {
			continue
		}
		if v.upNode && (allowReadOnly || !v.readOnly) {
			return v, true
		}
	}
	return &PodFact{}, false
}

// findPodToRunAdmintoolsAny returns the name of the pod we will exec into into
// order to run admintools.
// Will return false for second parameter if no pod could be found.
func (p *PodFacts) findPodToRunAdmintoolsAny() (*PodFact, bool) {
	// Our preference for the pod is as follows:
	// - up and not read-only
	// - up and read-only
	// - has vertica installation
	for _, v := range p.Detail {
		if v.upNode && !v.readOnly {
			return v, true
		}
	}
	for _, v := range p.Detail {
		if v.upNode {
			return v, true
		}
	}
	for _, v := range p.Detail {
		if v.isInstalled && v.isPodRunning {
			return v, true
		}
	}
	return &PodFact{}, false
}

// findPodToRunAdmintoolsOffline will return a pod to run an offline admintools
// command.  If nothing is found, the second parameter returned will be false.
func (p *PodFacts) findPodToRunAdmintoolsOffline() (*PodFact, bool) {
	for _, v := range p.Detail {
		if v.isInstalled && v.isPodRunning && !v.upNode {
			return v, true
		}
	}
	return &PodFact{}, false
}

// findRunningPod returns the first running pod.  If no pods are running, this
// return false.
func (p *PodFacts) findRunningPod() (*PodFact, bool) {
	for _, v := range p.Detail {
		if v.isPodRunning {
			return v, true
		}
	}
	return &PodFact{}, false
}

// findRestartablePods returns a list of pod facts that can be restarted.
// An empty list implies there are no pods that need to be restarted.
// We allow read-only nodes to be treated as being restartable because they are
// in the read-only state due to losing of cluster quorum.  This is an option
// for online upgrade, which want to keep the read-only up to keep the cluster
// accessible.
func (p *PodFacts) findRestartablePods(restartReadOnly, restartTransient bool) []*PodFact {
	return p.filterPods(func(v *PodFact) bool {
		if !restartTransient && v.isTransient {
			return false
		}
		return (!v.upNode || (restartReadOnly && v.readOnly)) && v.dbExists && v.isPodRunning && v.hasDCTableAnnotations
	})
}

// findInstalledPods returns a list of pods that have had the installer run
func (p *PodFacts) findInstalledPods() []*PodFact {
	return p.filterPods((func(v *PodFact) bool {
		return v.isInstalled && v.isPodRunning
	}))
}

// findReIPPods returns a list of pod facts that may need their IPs to be refreshed with re-ip.
// An empty list implies there are no pods that match the criteria.
func (p *PodFacts) findReIPPods(onlyPodsWithoutDBs bool) []*PodFact {
	return p.filterPods(func(pod *PodFact) bool {
		// Only consider running pods that exist and have an installation
		if !pod.exists || !pod.isPodRunning || !pod.isInstalled {
			return false
		}
		// If requested don't return pods that have a DB
		if onlyPodsWithoutDBs && pod.dbExists {
			return false
		}
		return true
	})
}

// filterPods return a list of PodFact that match the given filter.
// The filterFunc determines what pods to include.  If this function returns
// true, the pod is included.
func (p *PodFacts) filterPods(filterFunc func(p *PodFact) bool) []*PodFact {
	pods := []*PodFact{}
	for _, v := range p.Detail {
		if filterFunc(v) {
			pods = append(pods, v)
		}
	}
	return pods
}

// areAllPodsRunningAndZeroInstalled returns true if all of the pods are running
// and none of the pods have an installation.
func (p *PodFacts) areAllPodsRunningAndZeroInstalled() bool {
	for _, v := range p.Detail {
		if ((!v.exists || !v.isPodRunning) && v.managedByParent) || v.isInstalled {
			return false
		}
	}
	return true
}

// countPods is a generic function to do a count across the pod facts
func (p *PodFacts) countPods(countFunc func(p *PodFact) int) int {
	count := 0
	for _, v := range p.Detail {
		count += countFunc(v)
	}
	return count
}

// countRunningAndInstalled returns number of pods that are running and have an install
func (p *PodFacts) countRunningAndInstalled() int {
	return p.countPods(func(v *PodFact) int {
		if v.isPodRunning && v.isInstalled {
			return 1
		}
		return 0
	})
}

// countInstalledAndNotRestartable returns number of installed pods that aren't yet restartable
func (p *PodFacts) countInstalledAndNotRestartable() int {
	return p.countPods(func(v *PodFact) int {
		// We don't count non-running pods that aren't yet managed by the parent
		// sts.  The sts needs to be created or sized first.
		// We need the pod to have the DC table annotations since the DC
		// collection is done at start, so these need to set prior to starting.
		if v.isInstalled && v.managedByParent && (!v.isPodRunning || !v.hasDCTableAnnotations) {
			return 1
		}
		return 0
	})
}

// countUpPrimaryNodes returns the number of primary nodes that are UP
func (p *PodFacts) countUpPrimaryNodes() int {
	return p.countPods(func(v *PodFact) int {
		if v.upNode && v.isPrimary {
			return 1
		}
		return 0
	})
}

// countNotReadOnlyWithOldImage will return a count of the number of pods that
// are not read-only and are running an image different then newImage.  This is
// used in online upgrade to wait until pods running the old image have gone
// into read-only mode.
func (p *PodFacts) countNotReadOnlyWithOldImage(newImage string) int {
	return p.countPods(func(v *PodFact) int {
		if v.isPodRunning && v.upNode && !v.readOnly && v.image != newImage {
			return 1
		}
		return 0
	})
}

// getUpNodeCount returns the number of up nodes.
// A pod is considered down if it doesn't have a running vertica process.
func (p *PodFacts) getUpNodeCount() int {
	return p.countPods(func(v *PodFact) int {
		if v.upNode {
			return 1
		}
		return 0
	})
}

// getUpNodeAndNotReadOnlyCount returns the number of nodes that are up and
// writable.  Starting in 11.0SP2, nodes can be up but only in read-only state.
// This function filters out those *up* nodes that are in read-only state.
func (p *PodFacts) getUpNodeAndNotReadOnlyCount() int {
	return p.countPods(func(v *PodFact) int {
		if v.upNode && !v.readOnly {
			return 1
		}
		return 0
	})
}

// genPodNames will generate a string of pods names given a list of pods
func genPodNames(pods []*PodFact) string {
	podNames := make([]string, 0, len(pods))
	for _, pod := range pods {
		podNames = append(podNames, pod.name.Name)
	}
	return strings.Join(podNames, ", ")
}

// anyInstalledPodsNotRunning returns true if any installed pod isn't running.  It will
// return the name of the first pod that isn't running.
func (p *PodFacts) anyInstalledPodsNotRunning() (bool, types.NamespacedName) {
	for _, v := range p.Detail {
		if !v.isPodRunning && v.isInstalled {
			return true, v.name
		}
	}
	return false, types.NamespacedName{}
}

// anyUninstalledTransientPodsNotRunning will return true if it finds at least
// one transient pod that doesn't have an installation and isn't running.
func (p *PodFacts) anyUninstalledTransientPodsNotRunning() (bool, types.NamespacedName) {
	for _, v := range p.Detail {
		if v.isTransient && !v.isPodRunning && !v.isInstalled {
			return true, v.name
		}
	}
	return false, types.NamespacedName{}
}
