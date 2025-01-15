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
	"fmt"
	"slices"

	"github.com/vertica/vcluster/rfc7807"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type VUnsandboxOptions struct {
	DatabaseOptions
	SCName     string
	SCHosts    []string
	SCRawHosts []string
	// if restart the subcluster after unsandboxing it, the default value of it is true
	RestartSC bool
	// The expected node names with their IPs in the subcluster, the user of vclusterOps need
	// to make sure the provided values are correct. This option will be used to do re-ip in
	// the main cluster.
	NodeNameAddressMap map[string]string
	// A primary up host in the main cluster. This option will be used to do re-ip in
	// the main cluster.
	PrimaryUpHost string
}

func VUnsandboxOptionsFactory() VUnsandboxOptions {
	options := VUnsandboxOptions{}
	options.setDefaultValues()
	return options
}

func (options *VUnsandboxOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
	options.RestartSC = true
}

func (options *VUnsandboxOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(UnsandboxSCCmd, logger)
	if err != nil {
		return err
	}

	if options.SCName == "" {
		return fmt.Errorf("must specify a subcluster name")
	}

	err = util.ValidateScName(options.SCName)
	if err != nil {
		return err
	}
	return nil
}

func (options *VUnsandboxOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}
	return nil
}

// resolve hostnames to be IPs
func (options *VUnsandboxOptions) analyzeOptions() (err error) {
	// we analyze hostnames when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	// resolve SCRawHosts to be IP addresses
	if len(options.SCRawHosts) > 0 {
		options.SCHosts, err = util.ResolveRawHostsToAddresses(options.SCRawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VUnsandboxOptions) ValidateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// SubclusterNotSandboxedError is the error that is returned when
// the subcluster does not need unsandbox operation
type SubclusterNotSandboxedError struct {
	SCName string
}

func (e *SubclusterNotSandboxedError) Error() string {
	return fmt.Sprintf(`cannot unsandbox a regular subcluster [%s]`, e.SCName)
}

// info to be used while populating vdb
type ProcessedVDBInfo struct {
	// expect to fill the fields with info obtained from the main cluster
	MainClusterUpHosts            []string          // all UP hosts in the main cluster
	MainPrimaryUpHost             string            // primary UP host in the main cluster
	mainClusterNodeNameAddressMap map[string]string // NodeName to Address map for Main cluster nodes, this will be used to re-ip
	sandAddrsFromMain             []string          // all sandboxed IPs as collected from main cluster vdb
	sandScAddrsFromMain           []string          // all sandboxed subcluster IPs as collected from main cluster vdb
	SandboxName                   string            // name of sandbox which contains the subcluster to be unsandboxed
	ScFound                       bool              // is subcluster found in vdb

	// expect to fill the fields with info obtained from the sandbox
	UpSandboxHost               string            // UP host in the sandbox
	SandboxedHosts              []string          // All hosts in the sandbox which contains the subcluster to be unsandboxed
	SandboxedSubclusterHosts    []string          // All hosts in the subcluster to be unsandboxed
	SandboxedNodeNameAddressMap map[string]string // NodeName to Address map for sandboxed nodes, this will be used to re-ip
	upSCHosts                   []string          // subcluster hosts that are UP
	hasUpNodeInSC               bool              // if any node in the target subcluster is up. This is for internal use only.
}

// unsandboxPreCheck will build a list of instructions to perform
// unsandbox_subcluster pre-checks
//
// The generated instructions will later perform the following operations necessary
// for a successful unsandbox_subcluster
//   - Get cluster and nodes info from Main cluster (check if the DB is Eon)
//   - Get node info from sandboxed nodes
//   - validate if the subcluster to be unsandboxed exists and is sandboxed
//   - run re-ip on main cluster (with true ips of sandbox hosts) and on sandbox (with true ips of main cluster hosts)
//     This avoids issues when unsandboxed cluster joins the main cluster
func (vcc *VClusterCommands) unsandboxPreCheck(vdb *VCoordinationDatabase,
	options *VUnsandboxOptions,
	vdbInfo *ProcessedVDBInfo) error {
	// Get main cluster vdb
	err := vcc.getMainClusterVDB(vdb, options)
	if err != nil {
		return err
	}

	// Process main cluster nodes
	err = vcc.processMainClusterNodes(vdb, options, vdbInfo)
	if err != nil {
		return err
	}

	if !vdbInfo.ScFound {
		return vcc.handleSubclusterNotFound(options)
	}

	// Backup original main cluster HostNodeMap (deep copy)
	originalHostNodeMap := util.CloneMap(vdb.HostNodeMap, CloneVCoordinationNode)

	// Delete sandbox vals from vdb
	vdb.HostNodeMap = util.DeleteKeysFromMap(vdb.HostNodeMap, vdbInfo.sandAddrsFromMain)

	// Populate sandbox details
	err = vcc.updateSandboxDetails(vdb, options, vdbInfo)
	if err != nil {
		vcc.Log.PrintWarning("Failed to retrieve sandbox details for '%s', "+
			"main cluster might need to re-ip if sandbox host IPs have changed", vdbInfo.SandboxName)
		vdb.HostNodeMap = originalHostNodeMap
		vcc.updateSandboxDetailsFromMainCluster(vdb, options, vdbInfo)
	}

	// run re-ip on both of main cluster and the sandbox.
	err = vcc.reIPNodes(options, vdbInfo.UpSandboxHost, vdbInfo.MainPrimaryUpHost,
		vdbInfo.SandboxedNodeNameAddressMap, vdbInfo.mainClusterNodeNameAddressMap)
	if err != nil {
		return err
	}
	// Update options and finalize configuration
	options.Hosts = vdbInfo.MainClusterUpHosts
	return vcc.setClusterHosts(options, vdbInfo)
}

func (vcc *VClusterCommands) handleSubclusterNotFound(options *VUnsandboxOptions) error {
	vcc.Log.PrintError("Subcluster '%s' does not exist", options.SCName)
	rfcErr := rfc7807.New(rfc7807.SubclusterNotFound).WithHost(options.Hosts[0])
	return rfcErr
}

func (vcc *VClusterCommands) getMainClusterVDB(vdb *VCoordinationDatabase, options *VUnsandboxOptions) error {
	err := vcc.getVDBFromMainRunningDBContainsSandbox(vdb, &options.DatabaseOptions)
	if err != nil {
		return err
	}
	if !vdb.IsEon {
		return fmt.Errorf("cannot unsandbox subclusters for an enterprise database '%s'", options.DBName)
	}
	return nil
}

// update processed vdb info object and vdb with the sandbox details
func (vcc *VClusterCommands) updateSandboxDetails(
	vdb *VCoordinationDatabase,
	options *VUnsandboxOptions,
	info *ProcessedVDBInfo,
) error {
	info.SandboxedNodeNameAddressMap = make(map[string]string)
	sandVdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDBIncludeSandbox(&sandVdb, &options.DatabaseOptions, info.SandboxName)
	if err != nil {
		return err
	}
	// fill in the remainder of the fields of info not filled by main cluster
	for _, vnode := range sandVdb.HostNodeMap {
		if vnode.Sandbox == info.SandboxName {
			vdb.HostNodeMap[vnode.Address] = vnode
			info.SandboxedHosts = append(info.SandboxedHosts, vnode.Address)
		}
		if vnode.State == util.NodeUpState {
			info.hasUpNodeInSC = true
			info.upSCHosts = append(info.upSCHosts, vnode.Address)
			if vnode.IsPrimary {
				info.UpSandboxHost = vnode.Address
			}
		}
		if vnode.Subcluster == options.SCName {
			info.SandboxedSubclusterHosts = append(info.SandboxedSubclusterHosts, vnode.Address)
			info.SandboxedNodeNameAddressMap[vnode.Name] = vnode.Address
		}
	}
	return nil
}

// update processed vdb info object with the sandbox details from main cluster vdb
// this is used when there is no sandbox host in the hostlist or there are no UP sandbdox nodes.
func (vcc *VClusterCommands) updateSandboxDetailsFromMainCluster(
	vdb *VCoordinationDatabase,
	options *VUnsandboxOptions,
	info *ProcessedVDBInfo,
) {
	for _, vnode := range vdb.HostNodeMap {
		if vnode.Sandbox == info.SandboxName {
			info.SandboxedHosts = append(info.SandboxedHosts, vnode.Address)
		}
		if vnode.State != util.NodeDownState && vnode.Subcluster == options.SCName {
			info.hasUpNodeInSC = true
			info.upSCHosts = append(info.upSCHosts, vnode.Address)
			// reip requires an UP Primary node in the sandbox
			if vnode.IsPrimary && vnode.Sandbox == info.SandboxName {
				info.UpSandboxHost = vnode.Address
			}
			info.SandboxedSubclusterHosts = append(info.SandboxedSubclusterHosts, vnode.Address)
			info.SandboxedNodeNameAddressMap[vnode.Name] = vnode.Address
		}
	}
}

func (vcc *VClusterCommands) processMainClusterNodes(
	vdb *VCoordinationDatabase,
	options *VUnsandboxOptions,
	info *ProcessedVDBInfo,
) error {
	info.mainClusterNodeNameAddressMap = make(map[string]string)
	info.ScFound = false

	for _, vnode := range vdb.HostNodeMap {
		// Collect UP hosts and primary host
		if vnode.Sandbox == util.MainClusterSandbox {
			// Populate main cluster node map
			info.mainClusterNodeNameAddressMap[vnode.Name] = vnode.Address
			if vnode.State == util.NodeUpState {
				info.MainClusterUpHosts = append(info.MainClusterUpHosts, vnode.Address)
				if vnode.IsPrimary {
					info.MainPrimaryUpHost = vnode.Address
				}
			}
		}

		// Check for the specific subcluster
		if vnode.Subcluster == options.SCName {
			info.ScFound = true
			info.sandScAddrsFromMain = append(info.sandScAddrsFromMain, vnode.Address)
			if vnode.Sandbox == "" {
				return &SubclusterNotSandboxedError{SCName: options.SCName}
			}
			info.SandboxName = vnode.Sandbox
		}
	}
	// Update sandbox node details
	fetchAllSandHosts(vdb, info)
	return nil
}

func fetchAllSandHosts(vdb *VCoordinationDatabase, info *ProcessedVDBInfo) {
	for _, vnode := range vdb.HostNodeMap {
		if vnode.Sandbox == info.SandboxName {
			info.sandAddrsFromMain = append(info.sandAddrsFromMain, vnode.Address)
		}
	}
}
func (vcc *VClusterCommands) setClusterHosts(options *VUnsandboxOptions, info *ProcessedVDBInfo) error {
	options.SCHosts = info.sandScAddrsFromMain
	if len(info.SandboxedSubclusterHosts) > 0 {
		options.SCHosts = info.SandboxedSubclusterHosts
	}
	if len(info.MainClusterUpHosts) == 0 {
		return fmt.Errorf(`require at least one UP host outside of the sandbox subcluster '%s'in the input host list`, options.SCName)
	}
	return nil
}

func (vcc *VClusterCommands) reIPNodes(options *VUnsandboxOptions, upSandboxHost, mainPrimaryUpHost string,
	sandboxedNodeNameAddressMap, mainClusterNodeNameAddressMap map[string]string) error {
	// Skip re-ip if NodeNameAddressMap and PrimaryUpHost are already set
	if len(options.NodeNameAddressMap) > 0 && options.PrimaryUpHost != "" {
		return nil
	}

	// Handle re-ip on sandbox
	if upSandboxHost == "" {
		vcc.Log.PrintWarning("Skipping re-ip step on sandboxes as there are no UP nodes in the target sandbox.")
	} else {
		err := vcc.reIP(&options.DatabaseOptions, options.SCName, mainPrimaryUpHost, sandboxedNodeNameAddressMap, true /*reload spread*/)
		if err != nil {
			return fmt.Errorf("failed re-ip on sandbox: %w", err)
		}
		options.NodeNameAddressMap = sandboxedNodeNameAddressMap
	}

	// Handle reIP on main cluster
	if mainPrimaryUpHost == "" {
		vcc.Log.PrintWarning("Skipping re-ip step on main cluster as there are no primary UP nodes in the main cluster.")
	} else {
		err := vcc.reIP(&options.DatabaseOptions, "main cluster", upSandboxHost, mainClusterNodeNameAddressMap, true /*reload spread*/)
		if err != nil {
			return fmt.Errorf("failed re-ip on main cluster: %w", err)
		}
	}

	return nil
}

// produceUnsandboxSCInstructions will build a list of instructions to execute for
// the unsandbox subcluster operation.
//
// The generated instructions will later perform the following operations necessary
// for a successful unsandbox_subcluster:
//   - Get UP nodes through HTTPS call, if any node is UP then the DB is UP and ready for running unsandboxing operation
//     Also get up nodes from fellow subclusters in the same sandbox. Also get all UP nodes info in the given subcluster
//   - If the subcluster is UP
//     1. Stop the up subcluster hosts
//     2. Poll for stopped hosts to be down
//   - Run unsandboxing for the user provided subcluster using the selected initiator host(s).
//   - Remove catalog dirs from unsandboxed hosts
//   - VCluster CLI will restart the unsandboxed hosts using below instructions, but k8s operator will skip the restart process
//     1. Check Vertica versions
//     2. get start commands from UP main cluster node
//     3. run startup commands for unsandboxed nodes
//     4. Poll for started nodes to be UP
func (vcc *VClusterCommands) produceUnsandboxSCInstructions(options *VUnsandboxOptions, info *ProcessedVDBInfo) ([]clusterOp, error) {
	var instructions []clusterOp

	// when password is specified, we will use username/password to call https endpoints
	usePassword := false
	if options.Password != nil {
		usePassword = true
		err := options.validateUserName(vcc.Log)
		if err != nil {
			return instructions, err
		}
	}

	username := options.UserName
	// Check NMA health on sandbox hosts
	nmaHealthOp := makeNMAHealthOp(options.SCHosts)
	instructions = append(instructions, &nmaHealthOp)

	// Get all up nodes
	// options.Hosts has main cluster hosts and info.upSCHosts has UP Sandbox hosts, both of them
	// are used to update the execContext and used later in various unsandboxing related Ops
	allUpHosts := slices.Concat(options.Hosts, info.upSCHosts)
	httpsGetUpNodesOp, err := makeHTTPSGetUpScNodesOp(options.DBName, allUpHosts,
		usePassword, username, options.Password, UnsandboxSCCmd, options.SCName)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &httpsGetUpNodesOp)

	scHosts := []string{}
	scNodeNames := []string{}
	for nodeName, host := range options.NodeNameAddressMap {
		scHosts = append(scHosts, host)
		scNodeNames = append(scNodeNames, nodeName)
	}
	if info.hasUpNodeInSC {
		// Stop the nodes in the subcluster that is to be unsandboxed
		httpsStopNodeOp, e := makeHTTPSStopNodeOp(scHosts, scNodeNames, usePassword,
			username, options.Password, nil)
		if e != nil {
			return instructions, e
		}

		// Poll for nodes down
		httpsPollScDown, e := makeHTTPSPollSubclusterNodeStateDownOp(scHosts, options.SCName,
			usePassword, username, options.Password)
		if e != nil {
			return instructions, e
		}

		instructions = append(instructions,
			&httpsStopNodeOp,
			&httpsPollScDown,
		)
	}

	// Run Unsandboxing
	httpsUnsandboxSubclusterOp, err := makeHTTPSUnsandboxingOp(options.SCName,
		usePassword, username, options.Password, &options.SCHosts)
	if err != nil {
		return instructions, err
	}

	// Clean catalog dirs
	nmaDeleteDirsOp, err := makeNMADeleteDirsSandboxOp(scHosts, true, true /* sandbox */)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsUnsandboxSubclusterOp,
		&nmaDeleteDirsOp,
	)

	if options.RestartSC {
		// NMA check vertica versions before restart
		nmaVersionCheck := makeNMAVerticaVersionOpAfterUnsandbox(true, options.SCName)

		// Get startup commands
		httpsStartUpCommandOp, err := makeHTTPSStartUpCommandOpAfterUnsandbox(usePassword, username, options.Password)
		if err != nil {
			return instructions, err
		}

		// Start the nodes
		nmaStartNodesOp := makeNMAStartNodeOpAfterUnsandbox("")

		instructions = append(instructions,
			&nmaVersionCheck,
			&httpsStartUpCommandOp,
			&nmaStartNodesOp,
		)
	}

	return instructions, nil
}

func (vcc VClusterCommands) VUnsandbox(options *VUnsandboxOptions) error {
	vcc.Log.V(0).Info("VUnsandbox method called", "options", options)
	return runSandboxCmd(vcc, options)
}

// runCommand will produce instructions and run them
func (options *VUnsandboxOptions) runCommand(vcc VClusterCommands) error {
	// if the users want to do re-ip before unsandboxing, we require them
	// to provide some node information
	if options.PrimaryUpHost != "" && len(options.NodeNameAddressMap) > 0 {
		err := vcc.reIP(&options.DatabaseOptions, options.SCName, options.PrimaryUpHost,
			options.NodeNameAddressMap, true /*reload spread*/)
		if err != nil {
			return err
		}
	}

	vdb := makeVCoordinationDatabase()
	var vdbInfo ProcessedVDBInfo
	err := vcc.unsandboxPreCheck(&vdb, options, &vdbInfo)
	if err != nil {
		return err
	}
	// make instructions
	instructions, err := vcc.produceUnsandboxSCInstructions(options, &vdbInfo)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// add certs and instructions to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// run the engine
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to unsandbox subcluster %s, %w", options.SCName, runError)
	}

	// assume the caller knows the status of the cluster better than us, override whatever the unsandbox op set
	if len(options.NodeNameAddressMap) > 0 {
		options.SCHosts = []string{}
		for _, ip := range options.NodeNameAddressMap {
			options.SCHosts = append(options.SCHosts, ip)
		}
	}
	return nil
}
