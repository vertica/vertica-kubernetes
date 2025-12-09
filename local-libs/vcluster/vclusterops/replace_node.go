/*
 (c) Copyright [2023-2025] Open Text.
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
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// VReplaceNodeOptions represents the available options for VReplaceNode
type VReplaceNodeOptions struct {
	DatabaseOptions
	OriginalHost string
	NewHost      string
	Sandbox      string
	ForceDelete  bool

	// timeout for polling the node state after we start it
	TimeOut int
}

func VReplaceNodeOptionsFactory() VReplaceNodeOptions {
	options := VReplaceNodeOptions{}
	options.setDefaultValues()

	return options
}

func (options *VReplaceNodeOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VReplaceNodeOptions) validateEonOptions() error {
	if options.DepotPrefix != "" {
		return util.ValidateRequiredAbsPath(options.DepotPrefix, "depot path")
	}
	return nil
}

func (options *VReplaceNodeOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(ReplaceNodeCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VReplaceNodeOptions) validateExtraOptions() error {
	if options.DataPrefix != "" {
		return util.ValidateRequiredAbsPath(options.DataPrefix, "data path")
	}
	return nil
}

func (options *VReplaceNodeOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters
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

// analyzeOptions will modify some options based on what is chosen
func (options *VReplaceNodeOptions) analyzeOptions() (err error) {
	options.OriginalHost, err = util.ResolveToOneIP(options.OriginalHost, options.IPv6)
	if err != nil {
		return err
	}

	options.NewHost, err = util.ResolveToOneIP(options.NewHost, options.IPv6)
	if err != nil {
		return err
	}

	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	// resolve RawHosts to be IP addresses
	if len(options.RawHosts) > 0 {
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.normalizePaths()
	}

	return nil
}

func (options *VReplaceNodeOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	err := options.validateParseOptions(logger)
	if err != nil {
		return err
	}

	return options.analyzeOptions()
}

func (options *VReplaceNodeOptions) checkReplaceNodeRequirements(vdb *VCoordinationDatabase) error {
	// New host should be different than the original host
	if options.NewHost == options.OriginalHost {
		return fmt.Errorf("new host and original host must be different")
	}

	// The new host should not already be part of the DB
	if nodes, _ := vdb.containNodes([]string{options.NewHost}); len(nodes) != 0 {
		return fmt.Errorf("new host %s is already in the database", options.NewHost)
	}

	// The original host should be part of the DB
	if nodes, _ := vdb.containNodes([]string{options.OriginalHost}); len(nodes) != 1 {
		return fmt.Errorf("original host %s is not in the database", options.OriginalHost)
	}

	// The node should be down before running replace_node
	if vdb.hostIsUp(options.OriginalHost) {
		return fmt.Errorf("%s is up - stop the node before replacing it", options.OriginalHost)
	}

	return nil
}

// VReplaceNode replaces a database node with a new host
func (vcc VClusterCommands) VReplaceNode(options *VReplaceNodeOptions) (VCoordinationDatabase, error) {
	vdb := makeVCoordinationDatabase()

	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return vdb, err
	}

	if options.Sandbox != util.MainClusterSandbox {
		err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, options.Sandbox)
		if err != nil {
			return vdb, err
		}
	} else {
		err = vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
		if err != nil {
			return vdb, err
		}
	}

	err = options.completeVDBSetting(&vdb)
	if err != nil {
		return vdb, err
	}

	if vdb.IsEon {
		// checking this here because now we have got eon value from
		// the running db.
		if e := options.validateEonOptions(); e != nil {
			return vdb, e
		}
	}

	err = options.checkReplaceNodeRequirements(&vdb)
	if err != nil {
		return vdb, err
	}

	// Filter out non-sandbox hosts from vdb
	if options.Sandbox != util.MainClusterSandbox {
		vdb.filterSandboxNodes(options.Sandbox)
	} else {
		vdb.filterMainClusterNodes()
	}

	originalNode, ok := vdb.HostNodeMap[options.OriginalHost]
	if !ok {
		return vdb, fmt.Errorf("original host is not part of the cluster")
	}
	originalNode.Address = options.NewHost
	vdb.HostNodeMap[options.NewHost] = originalNode

	instructions, err := vcc.produceReplaceNodeInstructions(&vdb, options)
	if err != nil {
		return vdb, fmt.Errorf("fail to produce replace node instructions, %w", err)
	}

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	if runError := clusterOpEngine.run(vcc.Log); runError != nil {
		return vdb, fmt.Errorf("fail to complete replace node operation, %w", runError)
	}
	return vdb, nil
}

// completeVDBSetting sets some VCoordinationDatabase fields we cannot get yet
// from the https endpoints. We set those fields from options.
func (options *VReplaceNodeOptions) completeVDBSetting(vdb *VCoordinationDatabase) error {
	vdb.DataPrefix = options.DataPrefix
	vdb.DepotPrefix = options.DepotPrefix

	hostNodeMap := makeVHostNodeMap()
	// We could set the depot and data path from /nodes rather than manually.
	// This would be useful for nmaDeleteDirectoriesOp.
	for h, vnode := range vdb.HostNodeMap {
		dataPath := vdb.GenDataPath(vnode.Name)
		vnode.StorageLocations = append(vnode.StorageLocations, dataPath)
		if vdb.DepotPrefix != "" {
			vnode.DepotPath = vdb.GenDepotPath(vnode.Name)
		}
		hostNodeMap[h] = vnode
	}
	vdb.HostNodeMap = hostNodeMap

	return nil
}

// produceReplaceNodeInstructions will build a list of instructions to execute for
// the replace node operation.
//
// The generated instructions will later perform the following operations:
//   - Check NMA connectivity
//   - Check vertica versions - should be the same on all nodes
//   - Create directories on the new node
//   - Get network profiles
//   - Re-ip with replacement host
//   - Reload spread
//   - Transfer config files to the new node
//   - Start the new node
//   - Poll node startup
//   - Remove directories from the old node
func (vcc VClusterCommands) produceReplaceNodeInstructions(vdb *VCoordinationDatabase,
	options *VReplaceNodeOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	newHostList := []string{options.NewHost}

	// when password is specified, we will use username/password to call https endpoints
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}

	initiatorHost, err := getInitiatorHost(vdb.PrimaryUpNodes, []string{})
	if err != nil {
		return instructions, err
	}
	initiator := []string{initiatorHost}

	setupInstructions, err := vcc.produceReplaceNodeSetupInstructions(vdb, options)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, setupInstructions...)

	originalNode, ok := vdb.HostNodeMap[options.OriginalHost]
	if !ok {
		return instructions, fmt.Errorf("original host is not part of the cluster")
	}

	// Perform re-ip and reload spread
	httpsReIPOp, err := makeHTTPSReIPOpWithHosts(initiator, []string{originalNode.Name},
		[]string{options.NewHost}, options.usePassword, options.UserName, options.Password)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &httpsReIPOp)

	httpsReloadSpreadOp, err := makeHTTPSReloadSpreadOpWithInitiator(initiator, options.usePassword, options.UserName, options.Password)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &httpsReloadSpreadOp)

	// Update new vdb information after re-ip
	httpsGetNodesInfoOp, err := makeHTTPSGetNodesInfoOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password, vdb, true /*allow sandbox response*/, options.Sandbox)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &httpsGetNodesInfoOp)

	// Get startup command
	if options.Sandbox != util.MainClusterSandbox {
		httpsRestartUpCommandOp, err := makeHTTPSStartUpCommandWithSandboxOp(options.usePassword,
			options.UserName, options.Password, vdb, options.Sandbox)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsRestartUpCommandOp)
	} else {
		httpsRestartUpCommandOp, err := makeHTTPSStartUpCommandOp(options.usePassword, options.UserName, options.Password, vdb)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsRestartUpCommandOp)
	}

	// Send config to all current hosts - don't send to old node
	updatedHostList := options.getUpdatedHostList(vdb)

	produceTransferConfigOps(&instructions, nil, updatedHostList, vdb, /*db configurations retrieved from a running db*/
		&options.Sandbox)

	// Start the new node and poll until it's up
	nmaStartNewNodesOp := makeNMAStartNodeWithSandboxOpWithVDB(newHostList, "", options.Sandbox, vdb)
	var pollNodeStateOp clusterOp
	httpsPollNodeStateOp, err := makeHTTPSPollNodeStateOp(newHostList, options.usePassword, options.UserName,
		options.Password, options.TimeOut)
	if err != nil {
		return instructions, err
	}
	httpsPollNodeStateOp.cmdType = ReplaceNodeCmd
	pollNodeStateOp = &httpsPollNodeStateOp
	instructions = append(instructions,
		&nmaStartNewNodesOp,
		pollNodeStateOp,
	)

	cleanupInstructions, err := vcc.produceReplaceNodeCleanupInstructions(vdb, options)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, cleanupInstructions...)

	return instructions, nil
}

// produceReplaceNodeSetupInstructions will build a list of instructions that setup for a replace node operation
//
//   - Check NMA connectivity
//   - Check vertica versions - should be the same on all nodes
//   - Create directories on the new node
//   - Get network profiles
func (vcc VClusterCommands) produceReplaceNodeSetupInstructions(vdb *VCoordinationDatabase,
	options *VReplaceNodeOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	newHostList := []string{options.NewHost}

	nmaHealthOp := makeNMAHealthOp(vdb.HostList)
	instructions = append(instructions, &nmaHealthOp)

	// require to have the same vertica version
	nmaVerticaVersionOp := makeNMAVerticaVersionOpWithVDB(true /*hosts need to have the same Vertica version*/, vdb)
	instructions = append(instructions, &nmaVerticaVersionOp)

	// this is a copy of the original HostNodeMap that only contains the new host to add
	newHostNodeMap := vdb.copyHostNodeMap(newHostList)
	nmaPrepareDirectoriesOp, err := makeNMAPrepareDirsUseExistingDirOp(newHostNodeMap,
		false /*force cleanup*/, false /*for db revive*/, false /*useExistingCatalogDir?*/, false /*use existing depot dir*/)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &nmaPrepareDirectoriesOp)

	// Get network profile for re-ip
	nmaNetworkProfileOp := makeNMANetworkProfileOp(newHostList)
	instructions = append(instructions, &nmaNetworkProfileOp)

	return instructions, nil
}

// produceReplaceNodeCleanupInstructions will build a list of instructions that performs cleanup for a replace node operation
//
//   - Check NMA connectivity on old node
//   - Remove directories from the old node
func (vcc VClusterCommands) produceReplaceNodeCleanupInstructions(vdb *VCoordinationDatabase,
	options *VReplaceNodeOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	originalHostList := []string{options.OriginalHost}

	// Make a VDB copy with just the original host
	v := vdb.copy(originalHostList)
	originalHostNMAHealthOp := makeNMAHealthOpSkipUnreachable(v.HostList)
	instructions = append(instructions, &originalHostNMAHealthOp)

	nmaDeleteDirectoriesOp, err := makeNMADeleteDirectoriesOp(&v, true, /*forceDelete?*/
		false /*retainDirsExceptCatSubDir*/, false /*retainOnlyDepotDir*/)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &nmaDeleteDirectoriesOp)

	return instructions, nil
}

// Get the list of hosts in the cluster after replace node is done
func (options *VReplaceNodeOptions) getUpdatedHostList(vdb *VCoordinationDatabase) []string {
	updatedHostList := []string{}

	// Include all hosts except the old host
	for _, host := range vdb.HostList {
		if host != options.OriginalHost {
			updatedHostList = append(updatedHostList, host)
		}
	}
	// Include the new host
	updatedHostList = append(updatedHostList, options.NewHost)

	return updatedHostList
}
