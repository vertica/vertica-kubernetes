/*
 (c) Copyright [2023-2024] Open Text.
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

package vclusterops

import (
	"errors"
	"fmt"
	"path"
	"strings"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/vertica/vcluster/vclusterops/util"
)

const (
	ksafetyThreshold        = 3
	ksafeValueZero          = 0
	ksafeValueOne           = 1
	numOfAWSAuthComponents  = 2
	nmaSuccessfulReturnCode = 0
)

// produceTransferConfigOps generates instructions to transfert some config
// files from a sourceConfig node to target nodes.
func produceTransferConfigOps(instructions *[]clusterOp, sourceConfigHost,
	targetHosts []string, vdb *VCoordinationDatabase, sandbox *string) {
	var verticaConfContent string
	nmaDownloadVerticaConfigOp := makeNMADownloadConfigOp(
		"NMADownloadVerticaConfigOp", sourceConfigHost, "config/vertica", &verticaConfContent, vdb, sandbox)
	nmaUploadVerticaConfigOp := makeNMAUploadConfigOp(
		"NMAUploadVerticaConfigOp", sourceConfigHost, targetHosts, "config/vertica", &verticaConfContent, vdb)
	var spreadConfContent string
	nmaDownloadSpreadConfigOp := makeNMADownloadConfigOp(
		"NMADownloadSpreadConfigOp", sourceConfigHost, "config/spread", &spreadConfContent, vdb, sandbox)
	nmaUploadSpreadConfigOp := makeNMAUploadConfigOp(
		"NMAUploadSpreadConfigOp", sourceConfigHost, targetHosts, "config/spread", &spreadConfContent, vdb)
	*instructions = append(*instructions,
		&nmaDownloadVerticaConfigOp,
		&nmaUploadVerticaConfigOp,
		&nmaDownloadSpreadConfigOp,
		&nmaUploadSpreadConfigOp,
	)
}

// Get catalog path after we have db information from /catalog/database endpoint
func updateCatalogPathMapFromCatalogEditor(hosts []string, nmaVDB *nmaVDatabase, catalogPathMap map[string]string) error {
	if len(hosts) == 0 {
		return fmt.Errorf("[%s] fail to get host with highest catalog version", nmaVDB.Name)
	}
	for _, host := range hosts {
		vnode, ok := nmaVDB.HostNodeMap[host]
		if !ok {
			return fmt.Errorf("fail to get catalog path from host %s", host)
		}

		// catalog/database endpoint gets the catalog path as /data/{db_name}/v_{db_name}_node0001_catalog/Catalog
		// We need the parent dir of the full catalog path /data/{db_name}/v_{db_name}_node0001_catalog/
		catalogPathMap[host] = path.Dir(vnode.CatalogPath)
	}
	return nil
}

// Get primary nodes with latest catalog from catalog editor if the primaryHostsWithLatestCatalog info doesn't exist in execContext
func getPrimaryHostsWithLatestCatalog(nmaVDB *nmaVDatabase, hostsWithLatestCatalog []string, execContext *opEngineExecContext) []string {
	if len(execContext.primaryHostsWithLatestCatalog) > 0 {
		return execContext.primaryHostsWithLatestCatalog
	}
	emptyPrimaryHostsString := []string{}
	primaryHostsSet := mapset.NewSet[string]()
	for host, vnode := range nmaVDB.HostNodeMap {
		if vnode.IsPrimary {
			primaryHostsSet.Add(host)
		}
	}
	hostsWithLatestCatalogSet := mapset.NewSet(hostsWithLatestCatalog...)
	primaryHostsWithLatestCatalog := hostsWithLatestCatalogSet.Intersect(primaryHostsSet)
	primaryHostsWithLatestCatalogList := primaryHostsWithLatestCatalog.ToSlice()
	if len(primaryHostsWithLatestCatalogList) == 0 {
		return emptyPrimaryHostsString
	}
	execContext.primaryHostsWithLatestCatalog = primaryHostsWithLatestCatalogList // save the primaryHostsWithLatestCatalog to execContext
	return primaryHostsWithLatestCatalogList
}

// The following structs will store hosts' necessary information for https_get_up_nodes_op,
// https_get_nodes_information_from_running_db, and incoming operations.
type nodeStateInfo struct {
	Address          string   `json:"address"`
	State            string   `json:"state"`
	Database         string   `json:"database"`
	CatalogPath      string   `json:"catalog_path"`
	DepotPath        string   `json:"depot_path"`
	StorageLocations []string `json:"data_path"`
	Subcluster       string   `json:"subcluster_name"`
	IsPrimary        bool     `json:"is_primary"`
	Name             string   `json:"name"`
	Sandbox          string   `json:"sandbox_name"`
	Version          string   `json:"build_info"`
	IsControlNode    bool     `json:"is_control_node"`
	ControlNode      string   `json:"control_node"`
}

func (node *nodeStateInfo) asNodeInfo() (n NodeInfo, err error) {
	n = node.asNodeInfoWithoutVer()
	// version can be, eg, v24.0.0-<revision> or v23.4.0-<hotfix|date>-<revision> including a hotfix or daily build date
	verWithHotfix := 3
	verWithoutHotfix := 2
	if parts := strings.Split(node.Version, "-"); len(parts) == verWithHotfix {
		n.Version = parts[0] + "-" + parts[1]
	} else if len(parts) == verWithoutHotfix {
		n.Version = parts[0]
	} else {
		err = fmt.Errorf("failed to parse version '%s'", node.Version)
	}
	return
}

// asNodeInfoWithoutVer will create a NodeInfo with empty Version and Revision
func (node *nodeStateInfo) asNodeInfoWithoutVer() (n NodeInfo) {
	n.Address = node.Address
	n.Name = node.Name
	n.State = node.State
	n.CatalogPath = node.CatalogPath
	n.Subcluster = node.Subcluster
	n.IsPrimary = node.IsPrimary
	n.Sandbox = node.Sandbox
	return
}

type nodesStateInfo struct {
	NodeList []*nodeStateInfo `json:"node_list"`
}

// getInitiatorHost returns as initiator the first primary up node that is not
// in the list of hosts to skip.
func getInitiatorHost(primaryUpNodes, hostsToSkip []string) (string, error) {
	initiatorHosts := util.SliceDiff(primaryUpNodes, hostsToSkip)
	if len(initiatorHosts) == 0 {
		return "", fmt.Errorf("could not find any primary up nodes")
	}

	return initiatorHosts[0], nil
}

// getInitiatorHostInCluster returns an initiator that is the first up node of a subcluster in the main cluster
// or a sandbox other than the target subcluster
func getInitiatorHostInCluster(name, sandbox, scname string, vdb *VCoordinationDatabase) ([]string, error) {
	// up hosts will be :
	// 1. up hosts from the main subcluster if the sandbox is empty
	// 2. up hosts from the sandbox if the sandbox is specified
	var upHost string
	for _, node := range vdb.HostNodeMap {
		if node.State == util.NodeDownState {
			continue
		}
		// the up host is used to promote/demote subcluster
		// should not be a part of this subcluster
		if node.Sandbox == sandbox && node.Subcluster != scname {
			upHost = node.Address
			break
		}
	}
	if upHost == "" {
		if sandbox == "" {
			return nil, fmt.Errorf(`[%s] cannot find any up hosts for subcluster %s in main subcluster`, name, scname)
		}
		return nil, fmt.Errorf("[%s] cannot find any up hosts for subcluster %s in the sandbox %s", name, scname, sandbox)
	}
	// use first up host to execute https post request
	initiatorHost := []string{upHost}
	return initiatorHost, nil
}

// getInitiatorHostForReplication returns an initiator that is the first up source host in the main cluster
// or a sandbox
func getInitiatorHostForReplication(name, sandbox string, hosts []string, vdb *VCoordinationDatabase) ([]string, error) {
	// the k8s operator uses a service hostname, not an ip address of a node in the cluster
	// since the hostname will not match any node in the cluster we need to skip the below logic
	// this is ok since the operator has already chosen an appropriate "initiator"
	if util.IsK8sEnvironment() {
		return hosts, nil
	}
	// source hosts will be :
	// 1. up hosts from the main subcluster if the sandbox is empty
	// 2. up hosts from the sandbox if the sandbox is specified
	var sourceHosts []string
	for _, node := range vdb.HostNodeMap {
		if node.State != util.NodeDownState && node.Sandbox == sandbox {
			sourceHosts = append(sourceHosts, node.Address)
		}
	}
	sourceHosts = util.SliceCommon(hosts, sourceHosts)
	if len(sourceHosts) == 0 {
		if sandbox == "" {
			return nil, fmt.Errorf("[%s] cannot find any up hosts from source database", name)
		}
		return nil, fmt.Errorf("[%s] cannot find any up hosts in the sandbox %s", name, sandbox)
	}

	initiatorHost := []string{getInitiator(sourceHosts)}
	return initiatorHost, nil
}

// getVDBFromRunningDB will retrieve db configurations from a non-sandboxed host by calling https endpoints of a running db
func (vcc VClusterCommands) getVDBFromRunningDB(vdb *VCoordinationDatabase, options *DatabaseOptions) error {
	return vcc.getVDBFromRunningDBImpl(vdb, options, false /*allow use http result from sandbox nodes*/, util.MainClusterSandbox,
		false /*update node state by sending http request to each node*/)
}

// getVDBFromMainRunningDBContainsSandbox will retrieve db configurations from a non-sandboxed host by calling https endpoints of
// a running db, and it can return the accurate state of the sandboxed nodes.
func (vcc VClusterCommands) getVDBFromMainRunningDBContainsSandbox(vdb *VCoordinationDatabase, options *DatabaseOptions) error {
	return vcc.getVDBFromRunningDBImpl(vdb, options, false /*allow use http result from sandbox nodes*/, util.MainClusterSandbox,
		true /*update node state by sending http request to each node*/)
}

// getDeepVDBFromRunningDB will retrieve db config for main cluster nodes and all sandbox names from the main cluster,
// then fetch sandbox from each sandbox one by one separately and update the vdb object. This will return accurate cluster
// status if the main cluster is UP.
func (vcc VClusterCommands) getDeepVDBFromRunningDB(vdb *VCoordinationDatabase, options *DatabaseOptions) error {
	// Fetch vdb from main cluster
	mainVdb := makeVCoordinationDatabase()
	mainErr := vcc.getVDBFromRunningDBImpl(&mainVdb, options, false /*allow use http result from sandbox nodes*/, util.MainClusterSandbox,
		false /*update node state by sending http request to each node*/)
	if mainErr != nil {
		vcc.Log.Info("failed to get vdb info from main cluster, database could be down. Attempting to connect to sandboxes")
		return vcc.getVDBFromRunningDBImpl(vdb, options, true /*allow use http result from sandbox nodes*/, AnySandbox,
			false /*update node state by sending http request to each node*/)
	}
	// update vdb with main cluster info and retrieve all sandbox names
	vdb.setMainCluster(&mainVdb)
	// If we reach here, the main cluster is UP and we can fetch accurate vdb info from each sandbox separately
	for _, sandbox := range vdb.AllSandboxes {
		sandVdb := makeVCoordinationDatabase()
		sandErr := vcc.getVDBFromRunningDBImpl(&sandVdb, options, true /*allow use http result from sandbox nodes*/, sandbox,
			false /*update node state by sending http request to each node*/)
		if sandErr != nil {
			vcc.Log.Info("failed to get vdb info from sandbox %s", sandbox)
		} else {
			// update vdb with sandbox info
			vdb.updateSandboxNodeInfo(&sandVdb, sandbox)
		}
	}
	return nil
}

// getVDBFromRunningDBIncludeSandbox will retrieve db configurations from a sandboxed host by calling https endpoints of a running db
func (vcc VClusterCommands) getVDBFromRunningDBIncludeSandbox(vdb *VCoordinationDatabase, options *DatabaseOptions, sandbox string) error {
	return vcc.getVDBFromRunningDBImpl(vdb, options, true /*allow use http result from sandbox nodes*/, sandbox,
		false /*update node state by sending http request to each node*/)
}

// getVDBFromRunningDB will retrieve db configurations by calling https endpoints of a running db
func (vcc VClusterCommands) getVDBFromRunningDBImpl(vdb *VCoordinationDatabase, options *DatabaseOptions,
	allowUseSandboxRes bool, sandbox string, updateNodeState bool) error {
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return fmt.Errorf("fail to set userPassword while retrieving database configurations, %w", err)
	}

	httpsGetNodesInfoOp, err := makeHTTPSGetNodesInfoOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password, vdb, allowUseSandboxRes, sandbox)
	if err != nil {
		return fmt.Errorf("fail to produce httpsGetNodesInfo instruction while retrieving database configurations, %w", err)
	}

	httpsGetClusterInfoOp, err := makeHTTPSGetClusterInfoOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password, vdb)
	if err != nil {
		return fmt.Errorf("fail to produce httpsGetClusterInfo instruction while retrieving database configurations, %w", err)
	}

	var instructions []clusterOp
	instructions = append(instructions, &httpsGetNodesInfoOp, &httpsGetClusterInfoOp)

	// update node state for sandboxed nodes
	if allowUseSandboxRes || updateNodeState {
		httpsUpdateNodeState, e := makeHTTPSUpdateNodeStateOp(vdb, options.usePassword, options.UserName, options.Password)
		if e != nil {
			return fmt.Errorf("fail to produce httpsUpdateNodeState instruction while updating node states, %w", e)
		}
		instructions = append(instructions, &httpsUpdateNodeState)
	}

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	err = clusterOpEngine.run(vcc.Log)
	if err != nil {
		return fmt.Errorf("fail to retrieve database configurations, %w", err)
	}

	return nil
}

// getClusterInfoFromRunningDB will retrieve db configurations by calling https endpoints of a running db
func (vcc VClusterCommands) getClusterInfoFromRunningDB(vdb *VCoordinationDatabase, options *DatabaseOptions) error {
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return fmt.Errorf("fail to set userPassword while retrieving cluster configurations, %w", err)
	}

	httpsGetClusterInfoOp, err := makeHTTPSGetClusterInfoOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password, vdb)
	if err != nil {
		return fmt.Errorf("fail to produce httpsGetClusterInfo instructions while retrieving cluster configurations, %w", err)
	}

	var instructions []clusterOp
	instructions = append(instructions, &httpsGetClusterInfoOp)

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	err = clusterOpEngine.run(vcc.Log)
	if err != nil {
		return fmt.Errorf("fail to retrieve cluster configurations, %w", err)
	}

	return nil
}

// getCatalogPath returns the catalog path after
// removing `/Catalog` suffix (if present).
// It is useful when we get /data/{db_name}/v_{db_name}_node0001_catalog/Catalog
// and we want the parent dir of the full catalog path which is
// /data/{db_name}/v_{db_name}_node0001_catalog/
func getCatalogPath(fullPath string) string {
	if !strings.HasSuffix(fullPath, "/Catalog") {
		return fullPath
	}

	return path.Dir(fullPath)
}

// appendHTTPSFailureError is internally used by https operations for appending an error message to the existing error
func appendHTTPSFailureError(allErrs error) error {
	return errors.Join(allErrs, fmt.Errorf("could not find a host with a passing result"))
}

// getInitiator will pick an initiator from a host list to execute https calls
func getInitiator(hosts []string) string {
	// simply use the first one in user input
	return hosts[0]
}

// getInitiatorInCluster will pick an initiator from a host list to send requests to
// such that the host is up and within the specified cluster (either main or sandbox);
// an empty value of targetSandbox means the host is to be selected from the main cluster
func getInitiatorInCluster(targetSandbox string, hosts []string,
	upHostsToSandboxes map[string]string) (initiatorHost string, err error) {
	for _, host := range hosts {
		if sandbox, ok := upHostsToSandboxes[host]; ok && sandbox == targetSandbox {
			initiatorHost = host
			return
		}
	}
	if targetSandbox == "" {
		// main cluster
		err = fmt.Errorf("no hosts among %v are both UP and within the main cluster", hosts)
	} else {
		err = fmt.Errorf("no hosts among %v are both UP and within sandbox %v", hosts, targetSandbox)
	}
	return
}

// getInitiator will pick an initiator from the up host list to execute https calls
// such that the initiator is also among the user provided host list
func getInitiatorFromUpHosts(upHosts, userProvidedHosts []string) string {
	// Create a hash set for user-provided hosts
	userHostsSet := mapset.NewSet[string](userProvidedHosts...)

	// Iterate through upHosts and check if any host is in the userHostsSet
	for _, upHost := range upHosts {
		if userHostsSet.Contains(upHost) {
			return upHost
		}
	}

	// Return an empty string if no matching host is found
	return ""
}

// validates each host has an entry in each map
func validateHostMaps(hosts []string, maps ...map[string]string) error {
	var allErrors error
	for _, strMap := range maps {
		for _, host := range hosts {
			val, ok := strMap[host]
			if !ok || val == "" {
				allErrors = errors.Join(allErrors,
					fmt.Errorf("configuration map missing entry for host %s", host))
			}
		}
	}
	return allErrors
}

// reIP will do re-IP before sandboxing/unsandboxing if we find the catalog has stale node IPs.
// reIP will be called in three cases:
// 1. when sandboxing a subcluster, we will do re-ip in target sandbox since the node IPs in
// the main cluster could be changed. For example, a pod in main cluster gets restarted in k8s
// will cause inconsistent IPs between the sandbox and the main cluster. The target sandbox will
// have a stale node IP so adding that pod to the sandbox will fail.
// 2. when unsandboxing a subcluster, we will do re-ip in the main cluster since the node IPs
// in the sandbox could be changed. For example, a pod in a sandbox gets restarted in k8s will
// cause inconsistent IPs between the sandbox and the main cluster. The main cluster will
// have a stale node IP so moving that pod back to the main cluster will fail.
// 3. when removing a subcluster, we will do re-ip in the main cluster since the node IPs in
// the subcluster could be changed. This is a special case in k8s online upgrade, when a pod in
// a transient subcluster gets killed, we will not restart the pods in the subcluster. Instead,
// we will remove the subcluster. At this time, the nodes inside the subcluster have different IPs
// than the ones in the catalog, so removing subcluster will fail when deleting the catalog directories.
// We cannot find the correct nodes to do the deletion.
func (vcc *VClusterCommands) reIP(options *DatabaseOptions, scName, primaryUpHost string,
	nodeNameAddressMap map[string]string, reloadSpread bool) error {
	reIPList := []ReIPInfo{}
	reIPHosts := []string{}
	vdb := makeVCoordinationDatabase()

	backupHosts := options.Hosts
	// only use one up node in the sandbox/main-cluster to retrieve nodes' info,
	// then we can get the latest node IPs in the sandbox/main-cluster.
	// When the operation is sandbox, the initiator will be a primary up node
	// from the target sandbox.
	// When the operation is unsandbox, the initiator will be a primary up node
	// from the main cluster.
	// When the operation is remove_subcluster, the initiator will be a primary
	// up node from the main cluster.
	initiator := []string{primaryUpHost}
	options.Hosts = initiator
	err := vcc.getVDBFromRunningDBIncludeSandbox(&vdb, options, AnySandbox)
	if err != nil {
		return fmt.Errorf("host %q in database is not available: %w", primaryUpHost, err)
	}
	// restore the options.Hosts for later creating sandbox/unsandbox instructions
	options.Hosts = backupHosts

	// if the current node IPs doesn't match the expected ones, we need to do re-ip
	for _, vnode := range vdb.HostNodeMap {
		address, ok := nodeNameAddressMap[vnode.Name]
		if ok && address != vnode.Address {
			reIPList = append(reIPList, ReIPInfo{NodeName: vnode.Name, TargetAddress: address})
			reIPHosts = append(reIPHosts, address)
		}
	}
	if len(reIPList) > 0 {
		return vcc.doReIP(options, scName, initiator, reIPHosts, reIPList, reloadSpread)
	}
	return nil
}

// doReIP will call NMA and HTTPs endpoints to fix the IPs in the catalog.
// It will execute below steps:
// 1. collect network profile for the nodes that need to re-ip
// 2. execute re-ip on a primary up host
// 3. reload spread on a primary up host if needed
func (vcc *VClusterCommands) doReIP(options *DatabaseOptions, scName string,
	initiator, reIPHosts []string, reIPList []ReIPInfo, reloadSpread bool) error {
	var instructions []clusterOp
	nmaNetworkProfileOp := makeNMANetworkProfileOp(reIPHosts)
	err := options.setUsePassword(vcc.Log)
	if err != nil {
		return err
	}
	instructions = append(instructions, &nmaNetworkProfileOp)
	for _, reIPNode := range reIPList {
		httpsReIPOp, e := makeHTTPSReIPOpWithHosts(initiator, []string{reIPNode.NodeName},
			[]string{reIPNode.TargetAddress}, options.usePassword, options.UserName, options.Password)
		if e != nil {
			return e
		}
		instructions = append(instructions, &httpsReIPOp)
	}
	if reloadSpread {
		httpsReloadSpreadOp, e := makeHTTPSReloadSpreadOpWithInitiator(initiator, options.usePassword, options.UserName, options.Password)
		if e != nil {
			return err
		}
		instructions = append(instructions, &httpsReloadSpreadOp)
	}
	clusterOpEngine := makeClusterOpEngine(instructions, options)
	err = clusterOpEngine.run(vcc.Log)
	if err != nil {
		return fmt.Errorf("failed to re-ip nodes of subcluster %q: %w", scName, err)
	}

	return nil
}

func (vcc *VClusterCommands) getUnreachableHosts(options *DatabaseOptions, hosts []string) ([]string, error) {
	var nmaHealthInstructions []clusterOp
	nmaHealthOp := makeNMAHealthOpSkipUnreachable(hosts)
	nmaHealthInstructions = []clusterOp{&nmaHealthOp}
	opEng := makeClusterOpEngine(nmaHealthInstructions, options)
	err := opEng.run(vcc.Log)
	if err != nil {
		return nil, err
	}
	return opEng.execContext.unreachableHosts, nil
}

// An nmaGenericJSONResponse is the default response that is generated,
// the response value is of type "string" in JSON format.
type nmaGenericJSONResponse struct {
	RespStr string
}

// extractCatalogPrefix extracts the catalog prefix from a node's catalog path.
// This function takes the full catalog path, database name, and node name as
// input parameters, and returns the catalog prefix along with a boolean indicating
// whether the extraction was successful.
func extractCatalogPrefix(catalogPath, dbName, nodeName string) (string, bool) {
	catalogSuffix := "/" + dbName + "/" + nodeName + "_catalog/Catalog"
	// if catalog suffix matches catalog path, it means we created the catalog in the root path
	if catalogPath == catalogSuffix {
		return "/", true
	}
	if !strings.HasSuffix(catalogPath, catalogSuffix) {
		return "", false
	}
	return strings.TrimSuffix(catalogPath, catalogSuffix), true
}
