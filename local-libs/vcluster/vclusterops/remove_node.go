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
	"slices"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// VRemoveNodeOptions represents the available options to remove one or more nodes from
// the database.
type VRemoveNodeOptions struct {
	DatabaseOptions
	HostsToRemove        []string // Hosts to remove from database
	UnboundNodesToRemove []string // Unbound nodes to remove from database
	Initiator            string   // A primary up host that will be used to execute remove_node operations.
	ForceDelete          bool     // whether force delete directories
	IsSubcluster         bool     // is removing all nodes for a subcluster
	// Names of the nodes that need to have active subscription. The user of vclusterOps needs
	// to make sure the provided values are correct. This option will be used when some nodes
	// cannot join the main cluster so we will only check the node subscription state for the nodes
	// in this option. For example, after promote_sandbox, the nodes in old main cluster cannot
	// join the new main cluster so we should only check the node subscription state on the nodes
	// that are promoted from a sandbox.
	NodesToPullSubs []string
}

func VRemoveNodeOptionsFactory() VRemoveNodeOptions {
	options := VRemoveNodeOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VRemoveNodeOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()

	options.ForceDelete = true
	options.IsSubcluster = false
}

func (options *VRemoveNodeOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(RemoveNodeCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VRemoveNodeOptions) validateExtraOptions() error {
	// data prefix
	if options.DataPrefix != "" {
		return util.ValidateRequiredAbsPath(options.DataPrefix, "data path")
	}
	return nil
}

func (options *VRemoveNodeOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required params
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}
	// batch 2: validate all other params
	err = options.validateExtraOptions()
	if err != nil {
		return err
	}
	return nil
}

func (options *VRemoveNodeOptions) analyzeOptions() (err error) {
	options.HostsToRemove, err = util.ResolveRawHostsToAddresses(options.HostsToRemove, options.IPv6)
	if err != nil {
		return fmt.Errorf("cannot resolve the provided host addresses, detail: %w", err)
	}

	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.normalizePaths()
	}
	return nil
}

func (options *VRemoveNodeOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := options.validateParseOptions(log); err != nil {
		return err
	}
	err := options.analyzeOptions()
	if err != nil {
		return err
	}
	return options.setUsePasswordAndValidateUsernameIfNeeded(log)
}

func (vcc VClusterCommands) VRemoveNode(options *VRemoveNodeOptions) (VCoordinationDatabase, error) {
	vdb := makeVCoordinationDatabase()

	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return vdb, err
	}

	err = vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return vdb, err
	}

	err = options.completeVDBSetting(&vdb)
	if err != nil {
		return vdb, err
	}

	// remove_node is aborted if requirements are not met.
	err = checkRemoveNodeRequirements(&vdb, options)
	if err != nil {
		return vdb, err
	}
	// Figure out if the nodes to remove exist in the catalog. We follow
	// *normal* remove node logic if it still exists in the catalog. We tolerate
	// requests for nodes that aren't in the catalog because the caller may not
	// know (e.g. previous attempt to remove node didn't come back successful).
	// We have a simplified remove process for those requests to remove state
	// that the caller may be checking.
	var hostsNotInCatalog []string
	var reachableHostsNotInCatalog []string
	options.HostsToRemove, hostsNotInCatalog = vdb.containNodes(options.HostsToRemove)

	// Error out if there is unreachable nodes in input but not in catalog
	if len(hostsNotInCatalog) > 0 {
		var unreachableHosts []string
		unreachableHosts, err = vcc.getUnreachableHosts(&options.DatabaseOptions, hostsNotInCatalog)
		if err != nil {
			return vdb, fmt.Errorf("failed to get unreachable hosts, details: %w", err)
		}
		if len(unreachableHosts) > 0 {
			vcc.DisplayWarning("input hosts includes unreachable hosts not in the catalog: %v", unreachableHosts)
		}
		for _, host := range hostsNotInCatalog {
			if !slices.Contains(unreachableHosts, host) {
				reachableHostsNotInCatalog = append(reachableHostsNotInCatalog, host)
			}
		}
	}

	// remove unbound nodes from catalog
	if len(options.UnboundNodesToRemove) > 0 {
		err = vcc.removeUnboundNodesInCatalog(options, &vdb)
		if err != nil {
			return vdb, fmt.Errorf("failed to remove unbound nodes from catalog, details: %w", err)
		}
	}

	vdb, err = vcc.removeNodesInCatalog(options, &vdb)
	if err != nil || len(reachableHostsNotInCatalog) == 0 {
		return vdb, err
	}

	return vcc.handleRemoveNodeForHostsNotInCatalog(&vdb, options, reachableHostsNotInCatalog)
}

// removeUnboundNodesInCatalog removes unbound nodes from the catalog
func (vcc VClusterCommands) removeUnboundNodesInCatalog(options *VRemoveNodeOptions,
	vdb *VCoordinationDatabase) error {
	vlog.DisplayColorInfo("Removing unbound nodes from catalog")

	var instructions []clusterOp
	var initiatorHost []string

	usePassword := options.usePassword
	userName := options.UserName
	password := options.Password
	initiatorHost = append(initiatorHost, options.Initiator)

	unboundNodesInCatalog := mapset.NewSet[string]()
	for _, vnode := range vdb.UnboundNodes {
		unboundNodesInCatalog.Add(vnode.Name)
	}

	for _, unboundNode := range options.UnboundNodesToRemove {
		if !unboundNodesInCatalog.Contains(unboundNode) {
			return fmt.Errorf("%q is not an unbound node or does not exist in catalog", unboundNode)
		}

		httpsDropNodeOp, err := makeHTTPSDropNodeOp(unboundNode, initiatorHost,
			usePassword, userName, password,
			true /*cascade*/)
		if err != nil {
			return err
		}
		instructions = append(instructions, &httpsDropNodeOp)
	}

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	return clusterOpEngine.run(vcc.Log)
}

// removeNodesInCatalog will perform the steps to remove nodes. The node list in
// options.HostsToRemove has already been verified that each node is in the
// catalog.
func (vcc VClusterCommands) removeNodesInCatalog(options *VRemoveNodeOptions, vdb *VCoordinationDatabase) (VCoordinationDatabase, error) {
	if len(options.HostsToRemove) == 0 {
		vcc.Log.Info("Exit early because there are no hosts to remove")
		return *vdb, nil
	}
	vcc.Log.V(1).Info("validated input hosts", "HostsToRemove", options.HostsToRemove)

	vlog.DisplayColorInfo("Removing bound nodes from catalog")

	err := options.setInitiator(vdb.PrimaryUpNodes)
	if err != nil {
		return *vdb, err
	}

	instructions, err := vcc.produceRemoveNodeInstructions(vdb, options)
	if err != nil {
		return *vdb, fmt.Errorf("fail to produce remove node instructions, %w", err)
	}

	remainingHosts := util.SliceDiff(vdb.HostList, options.HostsToRemove)

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	if runError := clusterOpEngine.run(vcc.Log); runError != nil {
		// If the machines of the to-be-removed nodes crashed or get killed,
		// the run error may be ignored.
		// Here we check whether the to-be-removed nodes are still in the catalog.
		// If they have been removed from catalog, we let remove_node succeed.
		if vcc.findRemovedNodesInCatalog(options, remainingHosts) {
			return *vdb, fmt.Errorf("fail to complete remove node operation, %w", runError)
		}
		// If the target nodes have already been removed from catalog,
		// show a warning about the run error for users to trouble shoot their machines
		vcc.Log.PrintWarning("Nodes have been successfully removed, but encountered the following problems: %v",
			runError)
	}

	if len(clusterOpEngine.execContext.unreachableHosts) > 0 {
		vcc.DisplayInfo("Hint: please manually clean up directories in the unreachable host(s) %v",
			clusterOpEngine.execContext.unreachableHosts)
	}

	// we return a vdb that contains only the remaining hosts
	return vdb.copy(remainingHosts), nil
}

// handleRemoveNodeForHostsNotInCatalog will build and execute a list of
// instructions to do remove of hosts that aren't present in the catalog. We
// will do basic cleanup logic for this needed by the operator.
func (vcc VClusterCommands) handleRemoveNodeForHostsNotInCatalog(vdb *VCoordinationDatabase, options *VRemoveNodeOptions,
	missingHosts []string) (VCoordinationDatabase, error) {
	vcc.Log.Info("Doing cleanup of hosts missing from database", "hostsNotInCatalog", missingHosts)

	// We need to find the paths for the hosts we are removing.
	nmaGetNodesInfoOp := makeNMAGetNodesInfoOp(missingHosts, options.DBName, options.CatalogPrefix,
		false /* report all errors */, vdb)
	instructions := []clusterOp{&nmaGetNodesInfoOp}
	opEng := makeClusterOpEngine(instructions, options)
	err := opEng.run(vcc.Log)
	if err != nil {
		return *vdb, fmt.Errorf("failed to get node info for missing hosts: %w", err)
	}

	// Make a vdb of just the missing hosts. The host list for
	// nmaDeleteDirectoriesOp uses the host list from the vdb.
	vdbForDeleteDir := vdb.copy(missingHosts)
	err = options.completeVDBSetting(&vdbForDeleteDir)
	if err != nil {
		return *vdb, err
	}

	// Using the paths fetched earlier, we can now build the list of directories
	// that the NMA should remove.
	nmaDeleteDirectoriesOp, err := makeNMADeleteDirectoriesOp(&vdbForDeleteDir, options.ForceDelete)
	if err != nil {
		return *vdb, err
	}
	instructions = []clusterOp{&nmaDeleteDirectoriesOp}
	opEng = makeClusterOpEngine(instructions, options)
	err = opEng.run(vcc.Log)
	if err != nil {
		return *vdb, fmt.Errorf("failed to delete directories for missing hosts: %w", err)
	}

	remainingHosts := util.SliceDiff(vdb.HostList, missingHosts)
	return vdb.copy(remainingHosts), nil
}

// checkRemoveNodeRequirements validates any remove_node requirements. It will
// return an error if a requirement isn't met.
func checkRemoveNodeRequirements(vdb *VCoordinationDatabase, options *VRemoveNodeOptions) error {
	if !vdb.IsEon {
		if vdb.hasAtLeastOneDownNode() {
			return errors.New("all nodes must be up or standby")
		}
	}
	// cannot remove sandboxed nodes
	var sandboxedHosts []string
	for _, host := range options.HostsToRemove {
		vnode, ok := vdb.HostNodeMap[host]
		if ok && vnode.Sandbox != "" {
			sandboxedHosts = append(sandboxedHosts, fmt.Sprintf("%s (%s)", vnode.Name, vnode.Address))
		}
	}
	if len(sandboxedHosts) > 0 {
		return fmt.Errorf("hosts %v are sandboxed and cannot be removed", sandboxedHosts)
	}
	return nil
}

// completeVDBSetting sets some VCoordinationDatabase fields we cannot get yet
// from the https endpoints. We set those fields from options.
func (options *VRemoveNodeOptions) completeVDBSetting(vdb *VCoordinationDatabase) error {
	vdb.DataPrefix = options.DataPrefix

	if options.DepotPrefix == "" {
		return nil
	}
	if vdb.IsEon {
		// checking this here because now we have got eon value from
		// the running db. This will be removed once we are able to get
		// the depot path from db through an https endpoint(VER-88122).
		err := util.ValidateRequiredAbsPath(options.DepotPrefix, "depot path")
		if err != nil {
			return err
		}
	}
	vdb.DepotPrefix = options.DepotPrefix
	hostNodeMap := makeVHostNodeMap()
	// TODO: we set the depot path from /nodes rather than manually
	// (VER-92725). This is useful for nmaDeleteDirectoriesOp.
	for h, vnode := range vdb.HostNodeMap {
		vnode.DepotPath = vdb.GenDepotPath(vnode.Name)
		hostNodeMap[h] = vnode
	}
	vdb.HostNodeMap = hostNodeMap
	return nil
}

// this finds all main cluster UP nodes, regardless if they will be removed or not
func getMainClusterNodes(vdb *VCoordinationDatabase, options *VRemoveNodeOptions, mainClusterNodes, nodesToRemove *[]string) {
	// get nodes that will survive after removal, need to poll for ACTIVE subscriptions for those nodes
	hostsAfterRemoval := util.SliceDiff(vdb.HostList, options.HostsToRemove)
	for _, host := range hostsAfterRemoval {
		vnode := vdb.HostNodeMap[host]
		if vnode.Sandbox == util.MainClusterSandbox && vnode.State == util.NodeUpState {
			*mainClusterNodes = append(*mainClusterNodes, vnode.Name)
		}
	}
	// get nodes that will be removed to poll for REMOVING subscriptions
	for _, host := range options.HostsToRemove {
		vnode := vdb.HostNodeMap[host]
		*nodesToRemove = append(*nodesToRemove, vnode.Name)
	}
}

func getSortedHosts(hostsToRemove []string, hostNodeMap vHostNodeMap) []string {
	var sortedHosts []string
	for _, host := range hostsToRemove {
		if hostNodeMap[host].IsControlNode {
			sortedHosts = append(sortedHosts, host)
		} else {
			sortedHosts = append([]string{host}, sortedHosts...)
		}
	}
	return sortedHosts
}

// produceRemoveNodeInstructions will build a list of instructions to execute for
// the remove node operation.
//
// The generated instructions will later perform the following operations necessary
// for a successful remove_node:
//   - Update ksafety if needed
//   - Mark nodes to remove as ephemeral
//   - Rebalance cluster for Enterprise mode, rebalance shards for Eon mode
//   - Poll subscription state, wait for all subscrptions ACTIVE for Eon mode
//   - Remove secondary nodes from spread
//   - Drop Nodes
//   - Kill live compute nodes
//   - Poll compute node status
//   - Reload spread
//   - Delete catalog and data directories
//   - Sync catalog (eon only)
func (vcc VClusterCommands) produceRemoveNodeInstructions(vdb *VCoordinationDatabase, options *VRemoveNodeOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	var initiatorHost []string
	initiatorHost = append(initiatorHost, options.Initiator)

	username := options.UserName
	usePassword := options.usePassword
	password := options.Password

	// compute nodes don't count towards shard coverage
	permanentHostsToRemove := util.SliceDiff(options.HostsToRemove, vdb.ComputeNodes)

	// adjust k-safety if this remove node operation drops too many full nodes
	permanentHostCount := len(vdb.HostList) - len(vdb.ComputeNodes)
	if (permanentHostCount - len(permanentHostsToRemove)) < ksafetyThreshold {
		httpsMarkDesignKSafeOp, e := makeHTTPSMarkDesignKSafeOp(initiatorHost, usePassword, username,
			password, ksafeValueZero)
		if e != nil {
			return instructions, e
		}
		instructions = append(instructions, &httpsMarkDesignKSafeOp)
	}

	// compute nodes cannot be marked as ephemeral
	if len(permanentHostsToRemove) > 0 {
		err := vcc.produceMarkEphemeralNodeOps(&instructions, permanentHostsToRemove, initiatorHost,
			usePassword, username, password, vdb.HostNodeMap)
		if err != nil {
			return instructions, err
		}
	}

	// perform any rebalancing operations needed
	err := vcc.produceRebalanceClusterOps(&instructions, permanentHostsToRemove, initiatorHost, usePassword, username, password,
		vdb, options)
	if err != nil {
		return instructions, err
	}

	// only remove secondary nodes from spread
	err = vcc.produceSpreadRemoveNodeOp(&instructions, options.HostsToRemove,
		usePassword, username, password,
		initiatorHost, vdb.HostNodeMap)
	if err != nil {
		return instructions, err
	}

	sortedHosts := getSortedHosts(options.HostsToRemove, vdb.HostNodeMap)
	err = vcc.produceDropNodeOps(&instructions, sortedHosts, initiatorHost,
		usePassword, username, password, vdb.HostNodeMap, vdb.IsEon, options.IsSubcluster)
	if err != nil {
		return instructions, err
	}

	// compute nodes don't get the distcall to stop upon drop, so terminate them directly
	computeHostsToRemove := util.SliceCommon(options.HostsToRemove, vdb.ComputeNodes)
	err = vcc.produceStopAndPollComputeNodeOps(&instructions, computeHostsToRemove, vdb.HostNodeMap,
		usePassword, username, password)
	if err != nil {
		return instructions, nil
	}

	httpsReloadSpreadOp, err := makeHTTPSReloadSpreadOpWithInitiator(initiatorHost, usePassword, username, password)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &httpsReloadSpreadOp)

	// this is a copy of the original that only
	// contains the hosts to remove, including any compute nodes
	v := vdb.copy(options.HostsToRemove)
	nmaHealthOp := makeNMAHealthOpSkipUnreachable(v.HostList)
	nmaDeleteDirectoriesOp, err := makeNMADeleteDirectoriesOp(&v, options.ForceDelete)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &nmaHealthOp, &nmaDeleteDirectoriesOp)

	if vdb.IsEon {
		httpsSyncCatalogOp, err := makeHTTPSSyncCatalogOp(initiatorHost, usePassword, username, password, RemoveNodeSyncCat)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsSyncCatalogOp)
	}

	return instructions, nil
}

// produceMarkEphemeralNodeOps gets a slice of target hosts and for each of them
// produces an HTTPSMarkEphemeralNodeOp.
func (vcc VClusterCommands) produceMarkEphemeralNodeOps(instructions *[]clusterOp, targetHosts, hosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string,
	hostNodeMap vHostNodeMap) error {
	for _, host := range targetHosts {
		httpsMarkEphemeralNodeOp, err := makeHTTPSMarkEphemeralNodeOp(hostNodeMap[host].Name, hosts,
			useHTTPPassword, userName, httpsPassword)
		if err != nil {
			return err
		}
		*instructions = append(*instructions, &httpsMarkEphemeralNodeOp)
	}
	return nil
}

func (vcc VClusterCommands) produceRebalanceClusterOps(instructions *[]clusterOp,
	permanentHostsToRemove, initiatorHost []string,
	usePassword bool, username string, password *string,
	vdb *VCoordinationDatabase, options *VRemoveNodeOptions) error {
	if vdb.IsEon {
		// if all nodes being removed are compute nodes, we can skip rebalancing.
		if len(permanentHostsToRemove) > 0 {
			// we pass the set of subclusters of the permanent nodes to remove.
			v := vdb.copy(permanentHostsToRemove)
			err := vcc.produceRebalanceSubclusterShardsOps(instructions, initiatorHost, v.getSCNames(),
				usePassword, username, password)
			if err != nil {
				return err
			}
		}

		// For Eon DB, we check whether all UP nodes (nodesToPollSubs) have ACTIVE subscriptions after rebalancing shards.
		// Also wait til all REMOVING subscriptions are gone for the nodes to remove (nodesToRemove).
		// Sandboxed nodes cannot be removed, so even if the database has sandboxes,
		// polling subscriptions for the main cluster is enough.
		// This excludes compute nodes.
		var nodesToPollSubs, nodesToRemove []string
		if len(options.NodesToPullSubs) > 0 {
			nodesToPollSubs = options.NodesToPullSubs
		} else {
			getMainClusterNodes(vdb, options, &nodesToPollSubs, &nodesToRemove)
		}

		httpsPollSubscriptionStateOp, e := makeHTTPSPollSubscriptionStateOp(initiatorHost,
			usePassword, username, password, &nodesToPollSubs, &nodesToRemove)
		if e != nil {
			return e
		}
		*instructions = append(*instructions, &httpsPollSubscriptionStateOp)
	} else {
		var httpsRBCOp httpsRebalanceClusterOp
		httpsRBCOp, err := makeHTTPSRebalanceClusterOp(initiatorHost, usePassword, username,
			password)
		if err != nil {
			return err
		}
		*instructions = append(*instructions, &httpsRBCOp)
	}
	return nil
}

// produceRebalanceSubclusterShardsOps gets a slice of subclusters and for each of them
// produces an HTTPSRebalanceSubclusterShardsOp.
func (vcc VClusterCommands) produceRebalanceSubclusterShardsOps(instructions *[]clusterOp, initiatorHost, scNames []string,
	useHTTPPassword bool, userName string, httpsPassword *string) error {
	for _, scName := range scNames {
		if scName != "" {
			op, err := makeHTTPSRebalanceSubclusterShardsOp(
				initiatorHost, useHTTPPassword, userName, httpsPassword, scName)
			if err != nil {
				return err
			}
			*instructions = append(*instructions, &op)
		}
	}

	return nil
}

// produceDropNodeOps produces an HTTPSDropNodeOp for each node to drop.
// This is because we must drop node one by one to avoid losing quorum.
func (vcc VClusterCommands) produceDropNodeOps(instructions *[]clusterOp, targetHosts, hosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string,
	hostNodeMap vHostNodeMap, isEon bool, isSubcluster bool) error {
	for _, host := range targetHosts {
		httpsDropNodeOp, err := makeHTTPSDropNodeOp(hostNodeMap[host].Name, hosts,
			useHTTPPassword, userName, httpsPassword,
			isSubcluster || (isEon && hostNodeMap[host].State == util.NodeDownState))
		if err != nil {
			return err
		}
		*instructions = append(*instructions, &httpsDropNodeOp)
	}

	return nil
}

// produceStopAndPollComputeNodeOps produces the instructions to stop compute nodes
// and poll only the compute nodes for DOWN state.
func (vcc VClusterCommands) produceStopAndPollComputeNodeOps(instructions *[]clusterOp,
	computeHostsToStop []string,
	hostNodeMap vHostNodeMap,
	usePassword bool, username string, password *string) error {
	if len(computeHostsToStop) > 0 {
		err := vcc.produceStopComputeNodeOps(instructions, computeHostsToStop, hostNodeMap)
		if err != nil {
			return err
		}

		// Poll for compute nodes down
		httpsPollNodesDownOp, err := makeHTTPSPollNodeStateDownOp(computeHostsToStop,
			usePassword, username, password)
		if err != nil {
			return err
		}
		*instructions = append(*instructions, &httpsPollNodesDownOp)
	}
	return nil
}

// produceSpreadRemoveNodeOp calls HTTPSSpreadRemoveNodeOp
// when there is at least one secondary node to remove
func (vcc VClusterCommands) produceSpreadRemoveNodeOp(instructions *[]clusterOp, hostsToRemove []string,
	useHTTPPassword bool, userName string, httpsPassword *string,
	initiatorHost []string, hostNodeMap vHostNodeMap) error {
	// find secondary nodes from HostsToRemove
	var secondaryHostsToRemove []string
	for _, h := range hostsToRemove {
		vnode, ok := hostNodeMap[h]
		if !ok {
			return fmt.Errorf("cannot find host %s from vdb.HostNodeMap", h)
		}
		if !vnode.IsPrimary {
			secondaryHostsToRemove = append(secondaryHostsToRemove, h)
		}
	}

	// only call HTTPSSpreadRemoveNodeOp for secondary nodes to remove
	if len(secondaryHostsToRemove) > 0 {
		httpsSpreadRemoveNodeOp, err := makeHTTPSSpreadRemoveNodeOp(secondaryHostsToRemove, initiatorHost,
			useHTTPPassword, userName, httpsPassword, hostNodeMap)
		if err != nil {
			return err
		}
		*instructions = append(*instructions, &httpsSpreadRemoveNodeOp)
	}

	return nil
}

// setInitiator sets the initiator as the first primary up node that is not
// in the list of hosts to remove.
func (options *VRemoveNodeOptions) setInitiator(primaryUpNodes []string) error {
	initiatorHost, err := getInitiatorHost(primaryUpNodes, options.HostsToRemove)
	if err != nil {
		return err
	}
	options.Initiator = initiatorHost
	return nil
}

// findRemovedNodesInCatalog checks whether the to-be-removed nodes are still in catalog.
// Return true if they are still in catalog.
func (vcc VClusterCommands) findRemovedNodesInCatalog(options *VRemoveNodeOptions,
	remainingHosts []string) bool {
	fetchNodeStateOpt := VFetchNodeStateOptionsFactory()
	fetchNodeStateOpt.DBName = options.DBName
	fetchNodeStateOpt.RawHosts = remainingHosts
	fetchNodeStateOpt.IPv6 = options.IPv6
	fetchNodeStateOpt.UserName = options.UserName
	fetchNodeStateOpt.Password = options.Password

	var nodesInformation nodesInfo
	res, err := vcc.VFetchNodeState(&fetchNodeStateOpt)
	if err != nil {
		vcc.Log.PrintWarning("Fail to fetch states of the nodes, detail: %v", err)
		return false
	}
	nodesInformation.NodeList = res

	// return true if the target (to-be-removed) nodes are still in catalog
	return nodesInformation.findHosts(options.HostsToRemove)
}
