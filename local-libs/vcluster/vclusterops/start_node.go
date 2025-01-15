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

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// VStartNodesOptions represents the available options when you start one or more nodes
// with VStartNodes.
type VStartNodesOptions struct {
	// basic db info
	DatabaseOptions
	// A set of nodes (nodename - host) that we want to start in the database
	Nodes map[string]string
	// timeout for polling nodes that we want to start in httpsPollNodeStateOp
	StatePollingTimeout int
	// If the path is set, the NMA will store the Vertica start command at the path
	// instead of executing it. This is useful in containerized environments where
	// you may not want to have both the NMA and Vertica server in the same container.
	// This feature requires version 24.2.0+.
	StartUpConf string
	vdb         *VCoordinationDatabase
}

type VStartNodesInfo struct {
	// The IP address that we intend to re-IP can be obtained from a set of nodes provided as input
	// within VStartNodesOptions struct
	ReIPList []string
	// The names of the nodes that we intend to re-IP can be acquired from a set of nodes provided as input
	// within the VStartNodesOptions struct
	NodeNamesToStart []string
	// the hosts that we want to start
	HostsToStart []string
	// sandbox that we need to get nodes info from
	// empty string means that we need to get info from main cluster nodes
	Sandbox string
	// this can help decide whether there are nodes down that do not need to re-ip
	hasDownNodeNoNeedToReIP bool
	// hosts that are not reachable through NMA
	unreachableHosts []string
	// is start subcluster command
	isStartSc bool
	// use this when up host count is lesser than hosts to be re-ip'd
	SerialReIP bool
	// Number of up hosts
	upHostCount int
	// whether allow start unbound nodes individually
	// currently, we only allow the Kubernetes operator to do so
	DoAllowStartUnboundNodes bool
}

func VStartNodesOptionsFactory() VStartNodesOptions {
	options := VStartNodesOptions{}

	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VStartNodesOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
	// set default value to StatePollingTimeout
	options.StatePollingTimeout = util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", util.DefaultStatePollingTimeout)

	options.Nodes = make(map[string]string)
}

func (options *VStartNodesOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(StartNodeCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VStartNodesOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}
	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VStartNodesOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}
	return nil
}

// ParseNodesList resolves hostname in a nodeName-hostname map and build a new map.
// For example, map[string]string{vnodeName1: host1, vnodeName2: host2} is converted to
// map[string]string{vnodeName1: 192.168.1.101, vnodeName2: 192.168.1.102}
func (options *VStartNodesOptions) ParseNodesList(rawNodeMap map[string]string) error {
	for k, v := range rawNodeMap {
		ip, err := util.ResolveToOneIP(v, options.IPv6)
		if err != nil {
			return err
		}
		options.Nodes[k] = ip
	}
	return nil
}

func (options *VStartNodesOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

func (vcc VClusterCommands) startNodePreCheck(vdb *VCoordinationDatabase, options *VStartNodesOptions,
	hostNodeNameMap map[string]string, startNodeInfo *VStartNodesInfo) error {
	// sandboxs and the main cluster are not aware of each other's status
	// so check to make sure nodes to start are either
	// 1. all in the same sandbox, or
	// 2. all in main cluster
	sandboxNodeMap := make(map[string][]string)

	for nodename := range options.Nodes {
		oldIP, ok := hostNodeNameMap[nodename]
		if !ok {
			// silently skip nodes that are not in catalog
			continue
		}
		vnode := vdb.HostNodeMap[oldIP]
		sandboxNodeMap[vnode.Sandbox] = append(sandboxNodeMap[vnode.Sandbox], vnode.Name)
	}
	if len(sandboxNodeMap) > 1 {
		return fmt.Errorf(`cannot start nodes in different sandboxes, the sandbox-node map of the nodes to start is: %v`, sandboxNodeMap)
	}
	for k := range sandboxNodeMap {
		startNodeInfo.Sandbox = k
	}
	return nil
}

func (vcc VClusterCommands) removeUnreachableHosts(options *VStartNodesOptions) ([]string, error) {
	unreachableHosts, err := vcc.getUnreachableHosts(&options.DatabaseOptions, options.Hosts)
	if err != nil {
		return nil, err
	}
	options.Hosts = util.SliceDiff(options.Hosts, unreachableHosts)
	for _, unreachableHost := range unreachableHosts {
		for name, val := range options.Nodes {
			if val == unreachableHost {
				delete(options.Nodes, name)
			}
		}
	}
	return unreachableHosts, nil
}

func (vcc VClusterCommands) preStartNodeCheck(options *VStartNodesOptions, vdb *VCoordinationDatabase,
	hostNodeNameMap map[string]string, startNodeInfo *VStartNodesInfo) error {
	// retrieve database information to execute the command so we do not always rely on some user input
	// if VStartNodes is called from VStartSubcluster, we can reuse the vdb from VStartSubcluster
	if options.vdb == nil {
		err := vcc.getDeepVDBFromRunningDB(vdb, &options.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	for _, vnode := range vdb.HostNodeMap {
		hostNodeNameMap[vnode.Name] = vnode.Address
	}

	// precheck to make sure the nodes to start are either all sandboxed nodes in one sandbox or all main cluster nodes
	err := vcc.startNodePreCheck(vdb, options, hostNodeNameMap, startNodeInfo)
	if err != nil {
		return err
	}

	// if the nodes to be started are from main cluster, get vdb populated from a Main cluster node.
	if startNodeInfo.Sandbox == util.MainClusterSandbox {
		err = vcc.getVDBFromRunningDB(vdb, &options.DatabaseOptions)
	} else {
		err = vcc.getVDBFromRunningDBIncludeSandbox(vdb, &options.DatabaseOptions, startNodeInfo.Sandbox)
	}
	if err != nil {
		if startNodeInfo.Sandbox != util.MainClusterSandbox {
			return errors.Join(err, fmt.Errorf("hint: make sure there is at least one UP node in the sandbox %s", startNodeInfo.Sandbox))
		}
		return errors.Join(err, fmt.Errorf("hint: make sure there is at least one UP node in the database"))
	}

	// to avoid problems in the catalog, we only allow starting unbound nodes using start_subcluster
	// or when startNodeInfo.DoAllowStartUnboundNodes is true (which should only be set by the Kubernetes operator)
	var isStartingUnboundNodes bool
	for _, vnode := range vdb.UnboundNodes {
		_, toStart := options.Nodes[vnode.Name]
		if toStart {
			isStartingUnboundNodes = true
			break
		}
	}

	if isStartingUnboundNodes && (!startNodeInfo.isStartSc && !startNodeInfo.DoAllowStartUnboundNodes) {
		return errors.New("cannot directly start unbound nodes. " +
			"Please use start_subclusters to start unbound subclusters with new IP addresses")
	}

	return nil
}

// VStartNodes starts the given nodes for a cluster that has not yet lost
// cluster quorum. Returns any error encountered. If necessary, it updates the
// node's IP in the Vertica catalog. If cluster quorum is already lost, use
// VStartDatabase. It will skip any nodes given that no longer exist in the
// catalog.
func (vcc VClusterCommands) VStartNodes(options *VStartNodesOptions) error {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */
	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	_, err = vcc.removeUnreachableHosts(options)
	if err != nil || len(options.Nodes) == 0 {
		return err
	}

	startNodeInfo := new(VStartNodesInfo)
	vdb := makeVCoordinationDatabase()
	if options.vdb != nil {
		vdb = *options.vdb
		startNodeInfo.isStartSc = true
	}

	hostNodeNameMap := make(map[string]string)
	err = vcc.preStartNodeCheck(options, &vdb, hostNodeNameMap, startNodeInfo)
	if err != nil {
		return err
	}

	startNodeInfo.unreachableHosts, err = vcc.getUnreachableHosts(&options.DatabaseOptions, vdb.HostList)
	if err != nil {
		return err
	}

	// find out hosts
	// - that need to re-ip, and
	// - that don't need to re-ip
	startNodeInfo.HostsToStart, err = options.separateHostsBasedOnReIPNeed(hostNodeNameMap, startNodeInfo, &vdb, vcc.Log)
	if err != nil {
		return err
	}

	// check primary node count is more than nodes to re-ip, specially for sandboxes
	err = options.checkQuorum(&vdb, startNodeInfo)
	if err != nil {
		return err
	}

	// for the hosts that don't need to re-ip,
	// if none of them is down and no other nodes to re-ip,
	// we will early stop as there is no need to start them
	if !startNodeInfo.hasDownNodeNoNeedToReIP && len(startNodeInfo.ReIPList) == 0 {
		const msg = "The provided nodes are either not in catalog or already up. There is nothing to start."
		fmt.Println(msg)
		vcc.Log.Info(msg)
		return nil
	}

	// If no nodes found to start. We can simply exit here. This can happen if
	// given a list of nodes that aren't in the catalog any longer.
	if len(startNodeInfo.HostsToStart) == 0 {
		const msg = "None of the nodes provided are in the catalog. There is nothing to start."
		fmt.Println(msg)
		vcc.Log.Info(msg)
		return nil
	}

	// produce start_node instructions
	instructions, err := vcc.produceStartNodesInstructions(startNodeInfo, options, &vdb)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	err = clusterOpEngine.runInSandbox(vcc.Log, &vdb, startNodeInfo.Sandbox)
	if err != nil {
		return fmt.Errorf("fail to start node, %w", err)
	}
	return nil
}

// primary up node details can vary in case of sandboxes. This check is to ensure quorum is maintained
// even when a sandbox node is reip'ed
func (options *VStartNodesOptions) checkQuorum(vdb *VCoordinationDatabase, restartNodeInfo *VStartNodesInfo) error {
	sandboxPrimaryUpNodes := []string{}
	var lenOfPrimaryReIPLIst int
	upHostCount := 0
	reIPMap := make(map[string]bool, len(restartNodeInfo.ReIPList))
	for _, name := range restartNodeInfo.NodeNamesToStart {
		reIPMap[name] = true
	}
	for _, vnode := range vdb.HostNodeMap {
		if vnode.IsPrimary {
			if vnode.State == util.NodeUpState {
				upHostCount++
			}
			if vnode.State == util.NodeUpState && vnode.Sandbox == restartNodeInfo.Sandbox {
				sandboxPrimaryUpNodes = append(sandboxPrimaryUpNodes, vnode.Address)
			}
			if reIPMap[vnode.Name] {
				lenOfPrimaryReIPLIst++
			}
		}
	}
	restartNodeInfo.upHostCount = upHostCount
	if upHostCount < len(restartNodeInfo.ReIPList) {
		restartNodeInfo.SerialReIP = true
	}

	if len(sandboxPrimaryUpNodes) <= lenOfPrimaryReIPLIst && lenOfPrimaryReIPLIst > 0 {
		return &ReIPNoClusterQuorumError{
			Detail: fmt.Sprintf("Quorum check failed: %d up node(s) is/are not enough to re-ip %d primary node(s)",
				len(sandboxPrimaryUpNodes), lenOfPrimaryReIPLIst),
		}
	}
	return nil
}

// produceStartNodesInstructions will build a list of instructions to execute for
// the start_node command.
//
// The generated instructions will later perform the following operations necessary
// for a successful start_node:
//   - Check NMA connectivity
//   - Get UP nodes through HTTPS call, if any node is UP then the DB is UP and ready for starting nodes
//   - If need to do re-ip:
//     1. Call network profile
//     2. Call https re-ip endpoint
//     3. Reload spread
//     4. Call https /v1/nodes to update nodes' info
//   - Check Vertica versions
//   - Use any UP primary nodes as source host for syncing spread.conf and vertica.conf
//   - Sync the confs to the nodes to be started
//   - Call https /v1/startup/command to get start command of the nodes to be started
//   - start nodes
//   - Poll all node start up indirectly
//   - Poll permanent node start up directly
//   - sync catalog
func (vcc VClusterCommands) produceStartNodesInstructions(startNodeInfo *VStartNodesInfo, options *VStartNodesOptions,
	vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp
	nmaHealthOp := makeNMAHealthOpSkipUnreachable(options.Hosts)
	// need username for https operations
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}
	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password, StartNodeCmd)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions,
		&nmaHealthOp,
		&httpsGetUpNodesOp,
	)
	var sandboxName *string
	// If we identify any nodes that need re-IP, HostsToStart will contain the nodes that need re-IP.
	// Otherwise, HostsToStart will consist of all hosts with IPs recorded in the catalog, which are provided by user input.
	if len(startNodeInfo.ReIPList) != 0 {
		err = produceStartNodeReIPInstructions(&instructions, startNodeInfo, options, vdb)
		if err != nil {
			return instructions, err
		}
	} else {
		sandboxName = &startNodeInfo.Sandbox
	}

	// require to have the same vertica version
	nmaVerticaVersionOp := makeNMAVerticaVersionOpBeforeStartNode(vdb, startNodeInfo.unreachableHosts,
		startNodeInfo.HostsToStart, startNodeInfo.isStartSc)
	nmaCheckClusterVersionOp := makeNMACheckClusterVersionOp(options.Hosts, vdb, startNodeInfo.Sandbox)
	instructions = append(instructions, &nmaVerticaVersionOp, &nmaCheckClusterVersionOp)

	// The second parameter (sourceConfHost) in produceTransferConfigOps is set to a nil value in the upload and download step
	// we use information from v1/nodes endpoint to get all node information to update the sourceConfHost value
	// after we find any UP primary nodes as source host for syncing spread.conf and vertica.conf
	// we will remove the nil parameters in VER-88401 by adding them in execContext
	produceTransferConfigOps(&instructions, nil /*source hosts for transferring configuration files*/, startNodeInfo.HostsToStart,
		vdb, sandboxName)
	httpsRestartUpCommandOp, err := makeHTTPSStartUpCommandWithSandboxOp(options.usePassword, options.UserName, options.Password,
		vdb, startNodeInfo.Sandbox)
	if err != nil {
		return instructions, err
	}
	nmaStartNewNodesOp := makeNMAStartNodeOpWithVDB(startNodeInfo.HostsToStart, options.StartUpConf, vdb)
	permanentNodes := make([]string, 0, len(startNodeInfo.HostsToStart))
	httpsPollNodeStateIndirectOp, err := makeHTTPSPollUnknownNodeStateOp(startNodeInfo.HostsToStart,
		&permanentNodes, options.usePassword, options.UserName, options.Password,
		options.StatePollingTimeout)
	if err != nil {
		return instructions, err
	}
	httpsPollNodeStateOp, err := makeHTTPSPollPermanentNodeStateOp(startNodeInfo.HostsToStart,
		&permanentNodes, options.usePassword, options.UserName, options.Password,
		options.StatePollingTimeout)
	if err != nil {
		return instructions, err
	}
	httpsPollNodeStateOp.cmdType = StartNodeCmd
	instructions = append(instructions,
		&httpsRestartUpCommandOp,
		&nmaStartNewNodesOp,
		&httpsPollNodeStateIndirectOp,
		&httpsPollNodeStateOp,
	)
	if vdb.IsEon {
		httpsSyncCatalogOp, err := makeHTTPSSyncCatalogOp(options.Hosts, options.usePassword, options.UserName,
			options.Password, StartNodeSyncCat)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsSyncCatalogOp)
	}
	return instructions, nil
}

// If start_node needs to re-ip, we should:
// 1. call network profile
// 2. call https re-ip endpoint
// 3. reload spread
// 4. call https /v1/nodes to update nodes' info
func produceStartNodeReIPInstructions(instructions *[]clusterOp,
	startNodeInfo *VStartNodesInfo, options *VStartNodesOptions, vdb *VCoordinationDatabase) error {
	nmaNetworkProfileOp := makeNMANetworkProfileOp(startNodeInfo.ReIPList)
	*instructions = append(*instructions, &nmaNetworkProfileOp)
	if startNodeInfo.SerialReIP {
		// when we have lesser up(initiator) hosts than nodes to reip, we send reip requests in chunks of upHostCount size
		var reipOps []clusterOp
		chunkedNodeNamesTostart, chunkedReipList := getChunkedNodeLists(startNodeInfo)
		for i, hostChunk := range chunkedReipList {
			ReIPOp, err := makeHTTPSReIPOp(chunkedNodeNamesTostart[i], hostChunk,
				options.usePassword, options.UserName, options.Password)
			if err != nil {
				return err
			}
			reipOps = append(reipOps, &ReIPOp)
		}
		*instructions = append(*instructions, reipOps...)
	} else {
		httpsReIPOp, err := makeHTTPSReIPOp(startNodeInfo.NodeNamesToStart, startNodeInfo.ReIPList,
			options.usePassword, options.UserName, options.Password)
		if err != nil {
			return err
		}
		*instructions = append(*instructions, &httpsReIPOp)
	}
	// host is set to nil value in the reload spread step
	// we use information from node information to find the up host later
	httpsReloadSpreadOp, err := makeHTTPSReloadSpreadOp(options.usePassword, options.UserName, options.Password)
	if err != nil {
		return err
	}
	// update new vdb information after re-ip
	httpsGetNodesInfoOp, err := makeHTTPSGetNodesInfoOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password, vdb, options.usePassword, startNodeInfo.Sandbox)
	if err != nil {
		return err
	}
	*instructions = append(*instructions,
		&httpsReloadSpreadOp,
		&httpsGetNodesInfoOp,
	)

	return nil
}

func getChunkedNodeLists(startNodeInfo *VStartNodesInfo) (nodeNameChunks, reIPHostChunks [][]string) {
	chunkSize := startNodeInfo.upHostCount
	for i := 0; i < len(startNodeInfo.ReIPList); i += chunkSize {
		end := i + chunkSize
		if end > len(startNodeInfo.ReIPList) {
			end = len(startNodeInfo.ReIPList)
		}
		nodeNameChunks = append(nodeNameChunks, startNodeInfo.NodeNamesToStart[i:end])
		reIPHostChunks = append(reIPHostChunks, startNodeInfo.ReIPList[i:end])
	}
	return
}

// validateControlNode returns true if the host is a control node and error out
// if the host is a non-control node with its corresponding control node not up or not to be started
func (options *VStartNodesOptions) validateControlNode(host string, vdb *VCoordinationDatabase,
	hostNodeNameMap map[string]string, isReIP bool) (bool, error) {
	if vdb.HostNodeMap[host].IsControlNode {
		return true, nil
	}
	// non control node
	controlNode := vdb.HostNodeMap[host].ControlNode
	isInNodes := false
	if isReIP {
		for name, ip := range hostNodeNameMap {
			if controlNode == ip {
				_, isInNodes = options.Nodes[name]
				break
			}
		}
	} else {
		for _, ip := range options.Nodes {
			if controlNode == ip {
				isInNodes = true
				break
			}
		}
	}
	// control node is up or to be started
	if vdb.hostIsUp(controlNode) || isInNodes {
		// add non-control node to the end
		return false, nil
	}
	return false, fmt.Errorf("control node %s need to be up before node %s can be started", controlNode, host)
}

func (options *VStartNodesOptions) separateHostsBasedOnReIPNeed(
	hostNodeNameMap map[string]string,
	startNodeInfo *VStartNodesInfo,
	vdb *VCoordinationDatabase,
	logger vlog.Printer) ([]string, error) {
	var sortedHosts []string // control nodes first
	for nodename, newIP := range options.Nodes {
		oldIP, ok := hostNodeNameMap[nodename]
		if !ok {
			// We can get here if the caller requests a node that we were in the
			// middle of removing. Log a warning and continue without starting
			// that node.
			logger.Info("skipping start of node that doesn't exist in the catalog",
				"nodename", nodename, "newIP", newIP)
			continue
		}

		// if the IP that is given is different than the IP in the catalog, a re-ip is necessary
		if oldIP != newIP {
			startNodeInfo.ReIPList = append(startNodeInfo.ReIPList, newIP)
			startNodeInfo.NodeNamesToStart = append(startNodeInfo.NodeNamesToStart, nodename)
		} else {
			vnode, ok := vdb.HostNodeMap[newIP]
			if ok && vnode.State == util.NodeDownState {
				startNodeInfo.hasDownNodeNoNeedToReIP = true
			} else if ok && (vnode.State == util.NodeUpState || vnode.State == util.NodeComputeState) {
				// skip UP or COMPUTE nodes with no re-ip need
				continue
			}
		}

		isControl, err := options.validateControlNode(oldIP, vdb, hostNodeNameMap, oldIP != newIP)
		if err != nil {
			return sortedHosts, err
		}
		if isControl {
			// add control node to the front
			sortedHosts = append([]string{newIP}, sortedHosts...)
		} else {
			// add non-control node to the end
			sortedHosts = append(sortedHosts, newIP)
		}
	}

	// handle unbound nodes
	// some of the unbound nodes may need to re-ip
	for _, vnode := range vdb.UnboundNodes {
		if newIP, exists := options.Nodes[vnode.Name]; exists {
			startNodeInfo.ReIPList = append(startNodeInfo.ReIPList, newIP)
			startNodeInfo.NodeNamesToStart = append(startNodeInfo.NodeNamesToStart, vnode.Name)
			logger.DisplayInfo("the unbound node (%s) needs to change its IP to %s", vnode.Name, newIP)

			sortedHosts = append(sortedHosts, newIP)
		}
	}

	// log nodes that need to re-ip
	if len(startNodeInfo.NodeNamesToStart) > 0 {
		logger.Info("the nodes need to be re-IP", "nodeNames", startNodeInfo.NodeNamesToStart, "IPs", startNodeInfo.ReIPList)
	}

	return sortedHosts, nil
}
