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
	"sort"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
	"golang.org/x/exp/maps"
)

// VStartScOptions represents the available options when you start a subcluster
// from a database.
type VStartScOptions struct {
	DatabaseOptions
	VStartNodesOptions
	SCName      string   // subcluster to start
	NewHostList []string // expected to be already resolved IP addresses of new hosts used only for re-ip
	Sandbox     string
}

func VStartScOptionsFactory() VStartScOptions {
	options := VStartScOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VStartScOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
	options.VStartNodesOptions.setDefaultValues()
}

func (options *VStartScOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(StartSubclusterCmd, logger)
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

func (options *VStartScOptions) validateEonOptions() error {
	if !options.IsEon {
		return fmt.Errorf(`cannot start subcluster from an enterprise database '%s'`,
			options.DBName)
	}
	return nil
}

func (options *VStartScOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	// batch 2: validate eon parameters
	err = options.validateEonOptions()
	if err != nil {
		return err
	}
	return nil
}

func (options *VStartScOptions) analyzeOptions() (err error) {
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

func (options *VStartScOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	err := options.analyzeOptions()
	if err != nil {
		return err
	}
	return options.setUsePasswordAndValidateUsernameIfNeeded(logger)
}

// VStartSubcluster start nodes in a subcluster. It returns any error encountered.
// VStartSubcluster has two major phases:
//  1. Pre-check: check the subcluster name and get nodes for the subcluster.
//  2. Start nodes: Optional. If there are any down nodes in the subcluster, runs VStartNodes.
func (vcc VClusterCommands) VStartSubcluster(options *VStartScOptions) (VCoordinationDatabase, error) {
	// retrieve database information to execute the command so we do not always rely on some user input
	vdb := makeVCoordinationDatabase()

	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return vdb, err
	}

	sort.Strings(options.NewHostList)
	err = vcc.getVDBFromMainRunningDBContainsSandbox(&vdb, &options.DatabaseOptions)
	if err != nil {
		return vdb, err
	}
	err = options.validateNewHosts(&vdb, options.NewHostList)
	if err != nil {
		return vdb, err
	}

	nodesToStart := options.collectDownHosts(&vdb)
	if len(nodesToStart) == 0 {
		return vdb, fmt.Errorf("cannot find down node to start in subcluster %s ",
			options.SCName)
	}

	uniqueHosts := mapset.NewSet[string]()
	if len(options.NewHostList) > 0 {
		for _, host := range options.NewHostList {
			uniqueHosts.Add(host)
		}
		if uniqueHosts.Cardinality() < len(options.NewHostList) {
			return vdb, fmt.Errorf("number of provided unique hosts (%d) do not match number of total hosts (%d) to be started"+
				" in the subcluster %s", uniqueHosts.Cardinality(), len(nodesToStart), options.SCName)
		}
		if uniqueHosts.Cardinality() != len(nodesToStart) {
			return vdb, fmt.Errorf("number of hosts to be started (%d) do not match the number of down hosts (%d) in"+
				" the subcluster %s to be started", len(options.NewHostList), len(nodesToStart), options.SCName)
		}
		idx := 0
		for nodename := range nodesToStart {
			nodesToStart[nodename] = options.NewHostList[idx]
			idx++
		}
		err = options.checkPrepDirs(&vdb, nodesToStart, vcc.Log)
		if err != nil {
			return vdb, err
		}
	}

	options.VStartNodesOptions.Nodes = nodesToStart
	options.VStartNodesOptions.DatabaseOptions = options.DatabaseOptions
	options.VStartNodesOptions.StatePollingTimeout = options.StatePollingTimeout
	options.VStartNodesOptions.vdb = &vdb

	vlog.DisplayColorInfo("Starting nodes %v in subcluster %s", maps.Keys(nodesToStart), options.SCName)
	err = vcc.VStartNodes(&options.VStartNodesOptions)
	if err != nil {
		return vdb, err
	}
	err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, AnySandbox)
	return vdb, err
}

// Collect all down hosts that in the subcluster that need to be started
func (options *VStartScOptions) collectDownHosts(vdb *VCoordinationDatabase) (nodesToStart map[string]string) {
	nodesToStart = make(map[string]string)
	options.Sandbox = util.MainClusterSandbox
	// collect down nodes to start in the target subcluster
	for _, vnode := range vdb.HostNodeMap {
		if vnode.Subcluster == options.SCName {
			options.Sandbox = vnode.Sandbox
			if vnode.State == util.NodeDownState {
				// vnode.Address is the node IP address in the catalog
				nodesToStart[vnode.Name] = vnode.Address
			}
		}
	}
	// scan unbound nodes too
	for _, vnode := range vdb.UnboundNodes {
		if vnode.Subcluster == options.SCName {
			options.Sandbox = vnode.Sandbox
			if vnode.State == util.NodeDownState {
				// vnode.Address is a placeholder ip (0.0.0.0) which is later updated to the new IP
				// e.g. in checkPrepDirs
				nodesToStart[vnode.Name] = vnode.Address
			}
		}
	}
	return nodesToStart
}

func (options *VStartScOptions) validateNewHosts(vdb *VCoordinationDatabase, newHosts []string) error {
	for _, h := range newHosts {
		if _, exists := vdb.HostNodeMap[h]; exists {
			return fmt.Errorf("host %s is already a part of the database, please provide a new host ip to start the subcluster", h)
		}
	}
	return nil
}

// Unbound nodes that need to be started need to be re-ip'd and require node directories to be set up
func (options *VStartScOptions) checkPrepDirs(vdb *VCoordinationDatabase,
	nodesToStart map[string]string, logger vlog.Printer) error {
	unboundNodes := make(map[string]*VCoordinationNode)
	// scan unbound nodes
	for _, vnode := range vdb.UnboundNodes {
		if vnode.Subcluster == options.SCName && vnode.State == util.NodeDownState {
			// updating the new IP for unbound nodes
			vnode.Address = nodesToStart[vnode.Name]
			unboundNodes[nodesToStart[vnode.Name]] = vnode
		}
	}
	if len(unboundNodes) > 0 {
		var instructions []clusterOp
		nmaPrepDirsOp, err := makeNMAPrepareDirectoriesOp(unboundNodes, true /*force delete*/, false /*for db revive*/)
		if err != nil {
			return err
		}
		instructions = append(instructions, &nmaPrepDirsOp)
		// create a VClusterOpEngine, and add certs to the engine
		clusterOpEngine := makeClusterOpEngine(instructions, options)

		// give the instructions to the VClusterOpEngine to run
		err = clusterOpEngine.run(logger)
		if err != nil {
			return fmt.Errorf("fail to create directories on hosts to be started, %w", err)
		}
	}
	return nil
}
