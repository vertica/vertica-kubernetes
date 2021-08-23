/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"yunion.io/x/pkg/tristate"
)

// PodFact keeps track of facts for a specific pod
type PodFact struct {
	// Name of the pod
	name types.NamespacedName

	// dns name resolution of the pod
	dnsName string

	// IP address of the pod
	podIP string

	// Name of the subcluster the pod is part of
	subcluster string

	// true means the pod exists in k8s.  false means it hasn't been created yet.
	exists bool

	// true means the pod has been bound to a node, and all of the containers
	// have been created. At least one container is still running, or is in the
	// process of starting or restarting.
	isPodRunning bool

	// Have we run install for this pod? None means we are unable to determine
	// whether it is run.
	isInstalled tristate.TriState

	// Does admintools.conf exist but is for an old vdb?
	hasStaleAdmintoolsConf bool

	// Does the database exist at this pod? This is true iff the database was
	// created and this pod has been added to the vertica cluster.
	dbExists tristate.TriState

	// true means the pod has a running vertica process accepting connections on
	// port 5433.
	upNode bool

	// The vnode name that Vertica assigned to this pod.
	vnodeName string

	// The compat21 node name that Vertica assignes to the pod. This is only set
	// if installation has occurred.
	compat21NodeName string

	// True if the end user license agreement has been accepted
	eulaAccepted tristate.TriState

	// True if /opt/vertica/config/logrotate exists
	configLogrotateExists bool

	// True if /opt/vertica/config/logrotate is writable by dbadmin
	configLogrotateWritable bool

	// True if /opt/vertica/config/share exists
	configShareExists bool
}

type PodFactDetail map[types.NamespacedName]*PodFact

// A collection of facts for many pods.
type PodFacts struct {
	client.Client
	PRunner        cmds.PodRunner
	Detail         PodFactDetail
	NeedCollection bool
}

// MakePodFacts will create a PodFacts object and return it
func MakePodFacts(cli client.Client, prunner cmds.PodRunner) PodFacts {
	return PodFacts{Client: cli, PRunner: prunner, NeedCollection: true, Detail: make(PodFactDetail)}
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
	finder := MakeSubclusterFinder(p.Client, vdb)
	subclusters, err := finder.FindSubclusters(ctx, FindAll)
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
	if err := p.Client.Get(ctx, names.GenStsName(vdb, sc), sts); err != nil {
		// If the statefulset doesn't exist, none of the pods within it exist.  So fine to skip.
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("could not fetch statefulset for pod fact collection %s %w", sc.Name, err)
	}
	maxStsSize := sc.Size
	if *sts.Spec.Replicas > maxStsSize {
		maxStsSize = *sts.Spec.Replicas
	}

	for i := int32(0); i < maxStsSize; i++ {
		if err := p.collectPodByStsIndex(ctx, vdb, sc, i); err != nil {
			return err
		}
	}
	return nil
}

// collectPodByStsIndex will collect facts about a single pod in a subcluster
func (p *PodFacts) collectPodByStsIndex(ctx context.Context, vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) error {
	pf := PodFact{
		name:       names.GenPodName(vdb, sc, podIndex),
		subcluster: sc.Name,
	}

	pod := &corev1.Pod{}
	if err := p.Client.Get(ctx, pf.name, pod); errors.IsNotFound(err) {
		// Treat not found errors as if the pod is not running
		p.Detail[pf.name] = &pf
		return nil
	} else if err != nil {
		return err
	}
	pf.exists = true // Success from the Get() implies pod exists in API server
	pf.isPodRunning = pod.Status.Phase == corev1.PodRunning
	pf.dnsName = pod.Spec.Hostname + "." + pod.Spec.Subdomain
	pf.podIP = pod.Status.PodIP

	fns := []func(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error{
		p.checkIsInstalled,
		p.checkIsDBCreated,
		p.checkIfNodeIsUp,
		p.checkEulaAcceptance,
		p.checkLogrotateExists,
		p.checkIsLogrotateWritable,
		p.checkThatConfigShareExists,
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
	if pf.isPodRunning {
		fn := paths.GenInstallerIndicatorFileName(vdb)
		if stdout, stderr, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "cat", fn); err != nil {
			if !strings.Contains(stderr, "cat: "+fn+": No such file or directory") {
				return err
			}
			pf.isInstalled = tristate.False

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
			pf.isInstalled = tristate.True
			pf.compat21NodeName = strings.TrimSuffix(stdout, "\n")
		}
	} else {
		pf.isInstalled = tristate.None
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
	if pf.isPodRunning {
		if _, _, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "test", "-d", paths.ConfigLogrotatePath); err == nil {
			pf.configLogrotateExists = true
		}
	}
	return nil
}

// checkIsLogrotateWritable will verify that dbadmin has write access to /opt/vertica/config/logrotate
func (p *PodFacts) checkIsLogrotateWritable(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	if pf.isPodRunning {
		if _, _, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "test", "-w", paths.ConfigLogrotatePath); err == nil {
			pf.configLogrotateWritable = true
		}
	}
	return nil
}

// checkThatConfigShareExists will verify that /opt/vertica/config/share exists
func (p *PodFacts) checkThatConfigShareExists(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	if pf.isPodRunning {
		if _, _, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "test", "-d", paths.ConfigSharePath); err == nil {
			pf.configShareExists = true
		}
	}
	return nil
}

// checkIsDBCreated will check for evidence of a database at the local node.
// If a db is found, we will set the vertica node name.
func (p *PodFacts) checkIsDBCreated(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	if !pf.isPodRunning {
		pf.dbExists = tristate.None
		return nil
	}

	cmd := []string{
		"bash",
		"-c",
		fmt.Sprintf("ls -d %s/v_%s_node????_data", paths.GetDBDataPath(vdb), vdb.Spec.DBName),
	}
	if stdout, stderr, err := p.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, cmd...); err != nil {
		if !strings.Contains(stderr, "No such file or directory") {
			return err
		}
		pf.dbExists = tristate.False
	} else {
		pf.dbExists = tristate.True
		pf.vnodeName = parseVerticaNodeName(stdout)
	}

	return nil
}

// checkIfNodeIsUp will determine whether Vertica process is running in the pod.
func (p *PodFacts) checkIfNodeIsUp(ctx context.Context, vdb *vapi.VerticaDB, pf *PodFact) error {
	if pf.dbExists.IsFalse() || !pf.isPodRunning {
		pf.upNode = false
		return nil
	}

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

	return nil
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

// doesDBExist will check if the database exists anywhere.
// Returns tristate.False if we are 100% confident that the database doesn't
// exist anywhere. If we did not find any existence of database and at least one
// pod could not determine if db existed, the we return tristate.None.
func (p *PodFacts) doesDBExist() tristate.TriState {
	returnOnFail := tristate.False
	for _, v := range p.Detail {
		if v.dbExists.IsTrue() && v.isPodRunning {
			return tristate.True
		} else if v.dbExists.IsNone() || !v.isPodRunning {
			returnOnFail = tristate.None
		}
	}
	return returnOnFail
}

// anyPodsMissingDB will check whether each pod is added to the database
// It is a tristate return:
// - we return True if at least one runable pod doesn't have a database
// - we return False if all pods have a database
// - we return None if at least one pod we couldn't determine its state
func (p *PodFacts) anyPodsMissingDB(scName string) tristate.TriState {
	returnOnFail := tristate.False
	for _, v := range p.Detail {
		if v.subcluster != scName {
			continue
		}
		if v.dbExists.IsFalse() && v.isPodRunning {
			return tristate.True
		} else if v.dbExists.IsNone() {
			returnOnFail = tristate.None
		}
	}
	return returnOnFail
}

// findPodsWithMisstingDB will return a list of pods facts that have a missing DB
// It will only return pods that are running and that match the given
// subcluster. If no pods are found an empty list is returned. The list will be
// ordered by pod index.
func (p *PodFacts) findPodsWithMissingDB(scName string) []*PodFact {
	hostList := []*PodFact{}
	for _, v := range p.Detail {
		if v.subcluster != scName {
			continue
		}
		if v.dbExists.IsFalse() && v.isPodRunning {
			hostList = append(hostList, v)
		}
	}
	// Return an ordered list by pod index for easier debugging
	sort.Slice(hostList, func(i, j int) bool {
		return hostList[i].dnsName < hostList[j].dnsName
	})
	return hostList
}

// findPodToRunVsql returns the name of the pod we will exec into in
// order to run vsql
// Will return false for second parameter if no pod could be found.
func (p *PodFacts) findPodToRunVsql() (*PodFact, bool) {
	for _, v := range p.Detail {
		if v.upNode {
			return v, true
		}
	}
	return &PodFact{}, false
}

// findPodToRunAdmintools returns the name of the pod we will exec into into
// order to run admintools
// Will return false for second parameter if no pod could be found.
func (p *PodFacts) findPodToRunAdmintools() (*PodFact, bool) {
	// We prefer to pick a pod that is up.  But failing that, we will pick one
	// with vertica installed.
	for _, v := range p.Detail {
		if v.upNode {
			return v, true
		}
	}
	for _, v := range p.Detail {
		if v.isInstalled.IsTrue() && v.isPodRunning {
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
func (p *PodFacts) findRestartablePods() []*PodFact {
	return p.filterPods(func(v *PodFact) bool {
		return !v.upNode && v.dbExists.IsTrue() && v.isPodRunning
	})
}

// findInstalledPods returns a list of pods that have had the installer run
func (p *PodFacts) findInstalledPods() []*PodFact {
	return p.filterPods((func(v *PodFact) bool {
		return v.isInstalled.IsTrue() && v.isPodRunning
	}))
}

// findReIPPods returns a list of pod facts that may need their IPs to be refreshed with re-ip.
// An empty list implies there are no pods that match the criteria.
func (p *PodFacts) findReIPPods(onlyPodsWithoutDBs bool) []*PodFact {
	return p.filterPods(func(pod *PodFact) bool {
		// Only consider pods that exist and have an installation
		if !pod.exists || pod.isInstalled.IsFalse() {
			return false
		}
		// If requested don't return pods that have a DB
		if onlyPodsWithoutDBs && pod.dbExists.IsTrue() {
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

// countReipablePods returns number of pods that are running and have an install
func (p *PodFacts) countRunningAndInstalled() int {
	count := 0
	for _, v := range p.Detail {
		if v.isPodRunning && v.isInstalled.IsTrue() {
			count++
		}
	}
	return count
}

// getUpNodeCount returns the number of up nodes.
// A pod is considered down if it doesn't have a running vertica process.
func (p *PodFacts) getUpNodeCount() int {
	var count = 0
	for _, v := range p.Detail {
		if v.upNode {
			count++
		}
	}
	return count
}

// genPodNames will generate a string of pods names given a list of pods
func genPodNames(pods []*PodFact) string {
	podNames := make([]string, 0, len(pods))
	for _, pod := range pods {
		podNames = append(podNames, pod.name.Name)
	}
	return strings.Join(podNames, ", ")
}

// anyPodsNotRunning returns true if any pod exists but isn't running.  It will
// return the name of the first pod that isn't running.  This skips pods that
// haven't yet been created by Kubernetes -- only pods that exist will be checked.
func (p *PodFacts) anyPodsNotRunning() (bool, types.NamespacedName) {
	for _, v := range p.Detail {
		if v.exists && !v.isPodRunning {
			return true, v.name
		}
	}
	return false, types.NamespacedName{}
}
