package vclusterops

import (
	"errors"
	"fmt"

	"github.com/vertica/vcluster/rfc7807"
	"github.com/vertica/vcluster/vclusterops/util"
)

type VFetchNodeStateOptions struct {
	DatabaseOptions
	// retrieve the version for down nodes by invoking two additional
	// operations: NMAHealth and NMA readCatalogEditor. This is useful
	// when we cannot get the version for down nodes from a running database
	GetVersion bool

	SkipDownDatabase bool

	// only use this if options.RawHosts contains only sandboxed nodes
	SandboxedNodesOnly bool
}

func VFetchNodeStateOptionsFactory() VFetchNodeStateOptions {
	opt := VFetchNodeStateOptions{}
	// set default values to the params
	opt.setDefaultValues()

	return opt
}

func (options *VFetchNodeStateOptions) validateParseOptions(vcc VClusterCommands) error {
	if len(options.RawHosts) == 0 {
		return fmt.Errorf("must specify a host or host list")
	}

	if options.Password == nil {
		vcc.Log.PrintInfo("no password specified, using none")
	}

	return nil
}

func (options *VFetchNodeStateOptions) analyzeOptions() error {
	if len(options.RawHosts) > 0 {
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}
	return nil
}

func (options *VFetchNodeStateOptions) validateAnalyzeOptions(vcc VClusterCommands) error {
	if err := options.validateParseOptions(vcc); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VFetchNodeState returns the node state (e.g., up or down) for each node in the cluster and any
// error encountered.
func (vcc VClusterCommands) VFetchNodeState(options *VFetchNodeStateOptions) ([]NodeInfo, error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	err := options.validateAnalyzeOptions(vcc)
	if err != nil {
		return nil, err
	}

	// this vdb is used to fetch node version
	var vdb VCoordinationDatabase

	if options.SandboxedNodesOnly || util.IsK8sEnvironment() {
		err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, util.MainClusterSandbox)
	} else {
		err = vcc.getVDBFromMainRunningDBContainsSandbox(&vdb, &options.DatabaseOptions)
	}
	if err != nil {
		vcc.Log.PrintInfo("Error from vdb build: %s", err.Error())

		rfcError := &rfc7807.VProblem{}
		ok := errors.As(err, &rfcError)
		if ok {
			if rfcError.ProblemID == rfc7807.AuthenticationError {
				return nil, err
			}
		}

		if options.SkipDownDatabase {
			return []NodeInfo{}, rfc7807.New(rfc7807.FetchDownDatabase)
		}

		return vcc.fetchNodeStateFromDownDB(options)
	}

	nodeStates := buildNodeStateList(&vdb, false /*forDownDatabase*/)
	// return the result if no need to get version info
	if !options.GetVersion {
		return nodeStates, nil
	}

	// produce instructions to fill node information
	instructions, err := vcc.produceListAllNodesInstructions(options, &vdb)
	if err != nil {
		return nil, fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError == nil {
		// fill node version
		for i, nodeInfo := range nodeStates {
			vnode, ok := vdb.HostNodeMap[nodeInfo.Address]
			if ok {
				nodeStates[i].Version = vnode.Version
			} else {
				// we do not let this fail
				// but the version for this node will be empty
				vcc.Log.PrintWarning("Cannot find host %s in fetched node versions",
					nodeInfo.Address)
			}
		}
	}

	return nodeStates, nil
}

func (vcc VClusterCommands) fetchNodeStateFromDownDB(options *VFetchNodeStateOptions) ([]NodeInfo, error) {
	const msg = "Cannot get node information from running database. " +
		"Try to get node information by reading catalog editor.\n" +
		"The states of the nodes are shown as DOWN because we failed to fetch the node states."
	fmt.Println(msg)
	vcc.Log.PrintInfo(msg)

	var nodeStates []NodeInfo

	var fetchDatabaseOptions VFetchCoordinationDatabaseOptions
	fetchDatabaseOptions.DatabaseOptions = options.DatabaseOptions
	fetchDatabaseOptions.readOnly = true
	vdb, err := vcc.VFetchCoordinationDatabase(&fetchDatabaseOptions)
	if err != nil {
		return nodeStates, err
	}

	nodeStates = buildNodeStateList(&vdb, true /*forDownDatabase*/)

	return nodeStates, nil
}

// produceListAllNodesInstructions will build a list of instructions to execute for
// the fetch node state operation.
func (vcc VClusterCommands) produceListAllNodesInstructions(
	options *VFetchNodeStateOptions,
	vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp

	nmaHealthOp := makeNMAHealthOpSkipUnreachable(options.Hosts)
	nmaReadVerticaVersionOp := makeNMAReadVerticaVersionOp(vdb)

	if options.GetVersion {
		instructions = append(instructions,
			&nmaHealthOp,
			&nmaReadVerticaVersionOp)
	}

	return instructions, nil
}

func buildNodeStateList(vdb *VCoordinationDatabase, forDownDatabase bool) []NodeInfo {
	var nodeStates []NodeInfo

	// a map from a subcluster name to whether it is primary
	// Context: if a node is primary, the subcluster it belongs to is a primary subcluster.
	// If any of the nodes are down in such a primary subcluster, HTTPSUpdateNodeStateOp cannot correctly
	//   update its IsPrimary value, because this op sends request to each host.
	// We use the following scMap to check whether any node is primary in each subcluster,
	//   then update other nodes' IsPrimary value in this subcluster.
	scMap := make(map[string]bool)

	for _, h := range vdb.HostList {
		var nodeInfo NodeInfo
		n := vdb.HostNodeMap[h]
		nodeInfo.Address = n.Address
		nodeInfo.CatalogPath = n.CatalogPath
		nodeInfo.IsPrimary = n.IsPrimary
		nodeInfo.Name = n.Name
		nodeInfo.Sandbox = n.Sandbox
		if forDownDatabase && n.State == "" {
			nodeInfo.State = util.NodeDownState
		} else {
			nodeInfo.State = n.State
		}
		nodeInfo.Subcluster = n.Subcluster
		nodeInfo.Version = n.Version

		nodeStates = append(nodeStates, nodeInfo)

		if !forDownDatabase {
			if isPrimary, exists := scMap[n.Subcluster]; exists {
				scMap[n.Subcluster] = isPrimary || n.IsPrimary
			} else {
				scMap[n.Subcluster] = n.IsPrimary
			}
		}
	}

	for _, vnode := range vdb.UnboundNodes {
		var nodeInfo NodeInfo
		nodeInfo.Address = vnode.Address
		nodeInfo.CatalogPath = vnode.CatalogPath
		nodeInfo.IsPrimary = false
		nodeInfo.Name = vnode.Name
		nodeInfo.Sandbox = vnode.Sandbox
		nodeInfo.State = util.NodeDownState
		nodeInfo.Subcluster = vnode.Subcluster
		nodeStates = append(nodeStates, nodeInfo)
		scMap[vnode.Subcluster] = false
	}

	// update IsPrimary of the nodes for running database
	if !forDownDatabase {
		for i := 0; i < len(nodeStates); i++ {
			nodeInfo := nodeStates[i]
			scName := nodeInfo.Subcluster
			nodeStates[i].IsPrimary = scMap[scName]
		}
	}

	return nodeStates
}
