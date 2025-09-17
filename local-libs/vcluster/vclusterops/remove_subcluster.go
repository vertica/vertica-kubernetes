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
	"strings"

	"github.com/vertica/vcluster/rfc7807"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// VRemoveScOptions represents the available options when you remove a subcluster from a
// database.
type VRemoveScOptions struct {
	DatabaseOptions
	SCName      string // subcluster to remove from database
	ForceDelete bool   // whether force delete directories
	// The expected node names with their IPs in the subcluster, the user of vclusterOps needs
	// to make sure the provided values are correct. This option will be used to do re-ip in
	// the cluster that contains the subcluster.
	NodeNameAddressMap map[string]string
	// A primary up host in another subcluster that belongs to same cluster as the target subcluster.
	// This option will be used to do re-ip in the cluster.
	PrimaryUpHost string
	// Names of the nodes that need to have active subscription. The user of vclusterOps needs
	// to make sure the provided values are correct. This option will be used when some nodes
	// cannot join the main cluster so we will only check the node subscription state for the nodes
	// in this option. For example, after promote_sandbox, the nodes in old main cluster cannot
	// join the new main cluster so we should only check the node subscription state on the nodes
	// that are promoted from a sandbox.
	NodesToPullSubs []string
	// Stores the sandbox name if the subcluster to be removed is sandboxed
	Sandbox string
	// Stores main cluster primary UP hosts
	MainClusterUpPrimHosts []string
	// Whether we need to ignore main cluster while removing a sc from sandbox?
	IgnoreMainCluster bool
}

func VRemoveScOptionsFactory() VRemoveScOptions {
	options := VRemoveScOptions{}
	// set default values to the params
	options.setDefaultValues()

	options.ForceDelete = true

	return options
}

func (options *VRemoveScOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VRemoveScOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(RemoveSubclusterCmd, logger)
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

func (options *VRemoveScOptions) validateEonOptions() error {
	if !options.IsEon {
		return fmt.Errorf(`cannot remove subcluster from an enterprise database '%s'`,
			options.DBName)
	}
	return nil
}

func (options *VRemoveScOptions) validateExtraOptions() error {
	// VER-88096 will get data path and depot path from /nodes
	// so the validation below may be removed
	// data prefix
	err := util.ValidateRequiredAbsPath(options.DataPrefix, "data path")
	if err != nil {
		return err
	}

	// depot path
	return util.ValidateRequiredAbsPath(options.DepotPrefix, "depot path")
}

func (options *VRemoveScOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required params
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	// batch 2: validate eon params
	err = options.validateEonOptions()
	if err != nil {
		return err
	}

	// batch 3: validate all other params
	err = options.validateExtraOptions()
	if err != nil {
		return err
	}
	return nil
}

func (options *VRemoveScOptions) analyzeOptions() (err error) {
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

func (options *VRemoveScOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	err := options.analyzeOptions()
	if err != nil {
		return err
	}
	return options.setUsePasswordAndValidateUsernameIfNeeded(logger)
}

// VRemoveSubcluster removes a subcluster. It returns updated database catalog information and any error encountered.
// VRemoveSubcluster has three major phases:
//  1. Pre-check: check the subcluster name and get nodes for the subcluster.
//  2. Removes nodes: Optional. If there are any nodes still associated with the subcluster, runs VRemoveNode.
//  3. Drop the subcluster: Remove the subcluster name from the database catalog.
func (vcc VClusterCommands) VRemoveSubcluster(removeScOpt *VRemoveScOptions) (VCoordinationDatabase, error) {
	vdb := makeVCoordinationDatabase()

	// validate and analyze options
	err := removeScOpt.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return vdb, err
	}

	// If the users provide extra node information, we will check and do re-ip for the nodes in
	// the subcluster if necessary. This is to address the case where catalog has stale IPs of the
	// nodes in the subcluster, which would cause a node removal failure at delete-directory step.
	if removeScOpt.PrimaryUpHost != "" && len(removeScOpt.NodeNameAddressMap) > 0 {
		e := vcc.reIP(&removeScOpt.DatabaseOptions,
			removeScOpt.SCName,
			removeScOpt.PrimaryUpHost,
			removeScOpt.NodeNameAddressMap,
			// we will do reload spread in remove_node so we don't need to do reload spread here
			false /*reload spread*/)
		if e != nil {
			return vdb, e
		}
	}

	// pre-check: should not remove the default subcluster
	vcc.PrintInfo("Performing remove_subcluster pre-checks")
	hostsToRemove, unboundNodesToRemove, err := vcc.removeScPreCheck(&vdb, removeScOpt)
	if err != nil {
		return vdb, err
	}

	// proceed to run remove_node only if
	// the number of nodes to remove is greater than zero
	var needRemoveNodes bool
	vcc.Log.V(1).Info("Nodes to be removed: %+v", hostsToRemove)
	if len(hostsToRemove) == 0 && len(unboundNodesToRemove) == 0 {
		vcc.Log.PrintInfo("no node found in subcluster %s",
			removeScOpt.SCName)
		needRemoveNodes = false
	} else {
		needRemoveNodes = true
	}

	if needRemoveNodes {
		// Remove nodes from the target subcluster
		removeNodeOpt := VRemoveNodeOptionsFactory()
		removeNodeOpt.DatabaseOptions = removeScOpt.DatabaseOptions
		removeNodeOpt.HostsToRemove = hostsToRemove
		removeNodeOpt.UnboundNodesToRemove = unboundNodesToRemove
		removeNodeOpt.ForceDelete = removeScOpt.ForceDelete
		removeNodeOpt.IsSubcluster = true
		removeNodeOpt.NodesToPullSubs = removeScOpt.NodesToPullSubs

		// Get a set of initiators for removing nodes
		// The initiators are selected strictly from a sandbox if we are removing the sc from a sandbox
		// else it is a set of primary up nodes from the main cluster.
		initiators, err := removeScOpt.getAllInitiatorsOfTheOperatingDBGroup(&vdb, hostsToRemove)
		if err != nil {
			return vdb, err
		}
		removeNodeOpt.Initiators = initiators

		vcc.Log.PrintInfo("Removing nodes %q from subcluster %s using initiators %v",
			hostsToRemove, removeScOpt.SCName, removeNodeOpt.Initiators)
		if len(unboundNodesToRemove) > 0 {
			vcc.Log.PrintInfo("Removing unbound nodes %q from subcluster %s",
				unboundNodesToRemove, removeScOpt.SCName)
		}

		vdb, err = vcc.VRemoveNode(&removeNodeOpt)
		if err != nil {
			return vdb, err
		}
	}

	// drop subcluster (i.e., remove the sc name from catalog)
	vcc.Log.PrintInfo("Removing the subcluster name from catalog")
	err = vcc.dropSubcluster(&vdb, removeScOpt)
	if err != nil {
		return vdb, err
	}

	return vdb, nil
}

func (options *VRemoveScOptions) getAllInitiatorsOfTheOperatingDBGroup(vdb *VCoordinationDatabase, skipHosts []string) ([]string, error) {
	var initiator string
	var err error
	if options.Sandbox != util.MainClusterSandbox {
		initiator = vdb.getSandboxInitiator(options.Sandbox, skipHosts)
	} else {
		initiator, err = getInitiatorHost(vdb.PrimaryUpNodes, skipHosts)
		if err != nil {
			return []string{}, err
		}
	}

	initiators := []string{}
	if initiator != util.EmptyStringName {
		initiators = append(initiators, initiator)
	}

	if !options.IgnoreMainCluster && options.Sandbox != util.MainClusterSandbox {
		initiators = append(initiators, options.MainClusterUpPrimHosts[0])
	}
	return initiators, nil
}

type removeDefaultSubclusterError struct {
	Name string
}

func (e *removeDefaultSubclusterError) Error() string {
	return fmt.Sprintf("cannot remove the default subcluster '%s'", e.Name)
}

// removeScPreCheck will build a list of instructions to perform
// remove_subcluster pre-checks
//
// The generated instructions will later perform the following operations necessary
// for a successful remove_node:
//   - Get cluster and nodes info (check if the target DB is Eon and get to-be-removed node list)
//   - Get the subcluster info (check if the target sc exists and if it is the default sc)
func (vcc VClusterCommands) removeScPreCheck(
	vdb *VCoordinationDatabase,
	options *VRemoveScOptions) (hostsToRemove, unboundNodesToRemove []string, err error) {
	const preCheckErrMsg = "while performing remove_subcluster pre-checks"

	// get cluster and nodes info
	err = vcc.getDeepVDBFromRunningDB(vdb, &options.DatabaseOptions)
	if err != nil {
		return hostsToRemove, unboundNodesToRemove, err
	}

	// remove_subcluster only works with Eon database
	if !vdb.IsEon {
		// info from running db confirms that the db is not Eon
		return hostsToRemove, unboundNodesToRemove, fmt.Errorf(`cannot remove subcluster from an enterprise database '%s'`,
			options.DBName)
	}

	err = options.completeVDBSetting(vdb)
	if err != nil {
		return hostsToRemove, unboundNodesToRemove, err
	}

	options.filterHostsForSandbox(vdb)

	var instructions []clusterOp
	var ignoreNotFoundScErrs bool
	// Check if main cluster is aware of the sc to be removed
	// There could be a scenario where the target sc was added to a sandbox and the main cluster is unaware
	if options.Sandbox != util.MainClusterSandbox {
		ignoreNotFoundScErrs = true
	}
	httpsFindSubclusterInMainOp, err := makeHTTPSFindSubclusterOp(options.MainClusterUpPrimHosts,
		options.usePassword, options.UserName, options.Password,
		options.SCName,
		ignoreNotFoundScErrs /*ignore not found*/, RemoveSubclusterCmd)
	if err != nil {
		// The main cluster is unaware of the subcluster
		return hostsToRemove, unboundNodesToRemove, fmt.Errorf("fail to get default subcluster %s, details: %w",
			preCheckErrMsg, err)
	}
	instructions = append(instructions,
		&httpsFindSubclusterInMainOp,
	)
	if options.Sandbox != util.MainClusterSandbox {
		// Find default subcluster on sandbox hosts
		httpsFindSubclusterOp, err := makeHTTPSFindSubclusterOp(options.Hosts,
			options.usePassword, options.UserName, options.Password,
			options.SCName,
			!ignoreNotFoundScErrs /*do not ignore not found*/, RemoveSubclusterCmd)
		if err != nil {
			return hostsToRemove, unboundNodesToRemove, fmt.Errorf("fail to get default subcluster %s, details: %w",
				preCheckErrMsg, err)
		}
		instructions = append(instructions,
			&httpsFindSubclusterOp,
		)
	}

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	err = clusterOpEngine.run(vcc.Log)
	if err != nil {
		// VER-88585 will improve this rfc error flow
		if strings.Contains(err.Error(), "does not exist in the database") {
			vcc.Log.PrintError("fail to get subclusters' information %s, %v", preCheckErrMsg, err)
			rfcErr := rfc7807.New(rfc7807.SubclusterNotFound).WithHost(options.Hosts[0])
			return hostsToRemove, unboundNodesToRemove, rfcErr
		}
		return hostsToRemove, unboundNodesToRemove, err
	}

	// the default subcluster should not be removed
	if options.SCName == clusterOpEngine.execContext.defaultSCName {
		return hostsToRemove, unboundNodesToRemove, &removeDefaultSubclusterError{Name: options.SCName}
	}

	options.IgnoreMainCluster = clusterOpEngine.execContext.ignoreMainCluster

	// get nodes of the to-be-removed subcluster
	for h, vnode := range vdb.HostNodeMap {
		if vnode.Subcluster == options.SCName && options.Sandbox == vnode.Sandbox {
			hostsToRemove = append(hostsToRemove, h)
		}
	}

	for _, vnode := range vdb.UnboundNodes {
		unboundNodesToRemove = append(unboundNodesToRemove, vnode.Name)
	}

	return hostsToRemove, unboundNodesToRemove, nil
}

func (options *VRemoveScOptions) filterHostsForSandbox(vdb *VCoordinationDatabase) {
	var sandHosts []string
	for host, vnode := range vdb.HostNodeMap {
		// Always populate main cluster primaries
		if vnode.Sandbox == util.MainClusterSandbox && vnode.State == util.NodeUpState && vnode.IsPrimary {
			options.MainClusterUpPrimHosts = append(options.MainClusterUpPrimHosts, host)
		}
		// Skip if subcluster doesn't match
		if vnode.Subcluster != options.SCName {
			continue
		}
		// CASE 1: If --sandbox is provided, filter only from that sandbox
		if options.Sandbox != util.MainClusterSandbox && vnode.Sandbox == options.Sandbox {
			sandHosts = append(sandHosts, host)
		}
	}
	if len(sandHosts) > 0 {
		options.Hosts = sandHosts
	}
}

// completeVDBSetting sets some VCoordinationDatabase fields we cannot get yet
// from the https endpoints. We set those fields from options.
func (options *VRemoveScOptions) completeVDBSetting(vdb *VCoordinationDatabase) error {
	vdb.DataPrefix = options.DataPrefix
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

func (vcc VClusterCommands) dropSubcluster(vdb *VCoordinationDatabase, options *VRemoveScOptions) error {
	dropScErrMsg := fmt.Sprintf("fail to drop subcluster %s", options.SCName)
	// the initiator is a list of one primary up host per db group
	// that will call the https /v1/subclusters/{scName}/drop endpoint
	// as the endpoint will drop a subcluster, we only need one host per db group to do so
	initiators, err := options.getAllInitiatorsOfTheOperatingDBGroup(vdb, []string{})
	if err != nil {
		return err
	}

	httpsDropScOp, err := makeHTTPSDropSubclusterOp(initiators,
		options.SCName,
		options.usePassword, options.UserName, options.Password)
	if err != nil {
		vcc.Log.Error(err, "details: %v", dropScErrMsg)
		return err
	}

	var instructions []clusterOp
	instructions = append(instructions, &httpsDropScOp)

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	err = clusterOpEngine.run(vcc.Log)
	if err != nil {
		vcc.Log.Error(err, "fail to drop subcluster, details: %v", dropScErrMsg)
		return err
	}

	return nil
}
