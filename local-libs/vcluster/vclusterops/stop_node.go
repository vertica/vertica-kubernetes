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
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// VStopNodeOptions represents the available options for VStopNode.
type VStopNodeOptions struct {
	DatabaseOptions
	// Hosts to stop
	StopHosts []string
	// timeout for polling nodes that we want to wait in httpsPollNodeStopeOp
	StopPollingTimeout int
}

func VStopNodeOptionsFactory() VStopNodeOptions {
	options := VStopNodeOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VStopNodeOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
	// set time out from env variable
	options.StopPollingTimeout = util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", util.DefaultStatePollingTimeout)
}

func (options *VStopNodeOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(StopNodeCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VStopNodeOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}
	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VStopNodeOptions) analyzeOptions() (err error) {
	options.StopHosts, err = util.ResolveRawHostsToAddresses(options.StopHosts, options.IPv6)
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

func (options *VStopNodeOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	err := options.validateParseOptions(logger)
	if err != nil {
		return err
	}

	return options.analyzeOptions()
}

// VStopNode stops a host in an existing database.
// It returns any error encountered.
func (vcc VClusterCommands) VStopNode(options *VStopNodeOptions) error {
	vdb := makeVCoordinationDatabase()

	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	err = vcc.getDeepVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return err
	}

	// stop_node should fail if the nodes to stop belong to different sandboxes
	targetSandbox, err := checkTargetNodesSandboxes(&vdb, options)
	if err != nil {
		return err
	}

	options.completeVDBSetting(&vdb)

	// stop_node is aborted if requirements are not met.
	// Here we check whether the nodes to be stopped already exist
	err = checkStopNodeRequirements(&vdb, options.StopHosts)
	if err != nil {
		return err
	}

	instructions, err := vcc.produceStopNodeInstructions(&vdb, options)
	if err != nil {
		return fmt.Errorf("fail to produce stop node instructions, %w", err)
	}

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	if runError := clusterOpEngine.runInSandbox(vcc.Log, &vdb, targetSandbox); runError != nil {
		return fmt.Errorf("fail to complete stop node operation, %w", runError)
	}
	return nil
}

// checkStopNodeRequirements checks whether the nodes to stop belong to the same sandbox (or main cluster)
func checkTargetNodesSandboxes(vdb *VCoordinationDatabase,
	options *VStopNodeOptions) (targetSandbox string, err error) {
	sandboxHostMap := make(map[string][]string)
	for _, host := range options.StopHosts {
		vnode, exists := vdb.HostNodeMap[host]
		if !exists {
			return "", fmt.Errorf("cannot find host %s in vdb", host)
		}

		var sandboxDisplayName string
		if vnode.Sandbox == util.MainClusterSandbox {
			sandboxDisplayName = "main-cluster"
		} else {
			sandboxDisplayName = vnode.Sandbox
		}

		if _, exists := sandboxHostMap[sandboxDisplayName]; !exists {
			sandboxHostMap[sandboxDisplayName] = []string{host}
		} else {
			sandboxHostMap[sandboxDisplayName] = append(sandboxHostMap[sandboxDisplayName], host)
		}

		targetSandbox = vnode.Sandbox
	}
	if len(sandboxHostMap) > 1 {
		return "", fmt.Errorf("the hosts to stop cannot belong to different sandboxes %+v", sandboxHostMap)
	}

	return targetSandbox, nil
}

// checkStopNodeRequirements returns an error if at least one of the nodes
// to stop does not exist in db.
func checkStopNodeRequirements(vdb *VCoordinationDatabase, hostsToStop []string) error {
	// the host to be stopped should be a part of the database.
	if nodes, _ := vdb.containNodes(hostsToStop); len(nodes) == 0 {
		return fmt.Errorf("%s do not exist in the database", strings.Join(nodes, ","))
	}

	return nil
}

// completeVDBSetting sets some VCoordinationDatabase fields we cannot get yet
// from the https endpoints. We set those fields from options.
func (options *VStopNodeOptions) completeVDBSetting(vdb *VCoordinationDatabase) {
	hostNodeMap := makeVHostNodeMap()
	for h, vnode := range vdb.HostNodeMap {
		hostNodeMap[h] = vnode
	}
	vdb.HostNodeMap = hostNodeMap
}

// produceStopNodeInstructions will build a list of instructions to execute for
// the stop node operation.
//
// The generated instructions will later perform the following operations necessary
// for a successful stop_node:
//   - Stop nodes
//   - Poll node state down
func (vcc VClusterCommands) produceStopNodeInstructions(vdb *VCoordinationDatabase,
	options *VStopNodeOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	username := options.UserName
	usePassword := options.usePassword
	password := options.Password

	// most node types can be stopped via HTTPS service
	regularHostsToStop := util.SliceDiff(options.StopHosts, vdb.ComputeNodes)
	if len(regularHostsToStop) > 0 {
		regularStopHostNodeNameMap := make(map[string]string)
		regularStopHostNodeMap := vdb.copyHostNodeMap(regularHostsToStop)
		for h, vnode := range regularStopHostNodeMap {
			regularStopHostNodeNameMap[vnode.Name] = h
		}

		httpsStopNodeOp, err := makeHTTPSStopInputNodesOp(regularStopHostNodeNameMap, usePassword, username, password, nil)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsStopNodeOp)
	}

	// compute nodes currently don't support distcalls, so need to kill by signal via NMA
	computeHostsToStop := util.SliceCommon(options.StopHosts, vdb.ComputeNodes)
	if len(computeHostsToStop) > 0 {
		err := vcc.produceStopComputeNodeOps(&instructions, computeHostsToStop, vdb.HostNodeMap)
		if err != nil {
			return instructions, err
		}
	}

	// Poll for nodes down
	httpsPollNodesDown, err := makeHTTPSPollNodeStateDownOp(options.StopHosts,
		usePassword, username, password)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions, &httpsPollNodesDown)
	return instructions, nil
}

// produceStopComputeNodeOps creates the instructions required to terminate compute nodes.
// Since compute nodes lack distcall support, they must be stopped via signal, including when
// dropped.
func (vcc VClusterCommands) produceStopComputeNodeOps(instructions *[]clusterOp,
	computeHostsToStop []string,
	hostNodeMap vHostNodeMap) error {
	nmaHealthOp := makeNMAHealthOp(computeHostsToStop)
	computeHostCatPathMap := make(map[string]string, len(computeHostsToStop))
	for _, host := range computeHostsToStop {
		computeHostCatPathMap[host] = hostNodeMap[host].CatalogPath
	}
	nmaSigTermNodeOp, err := makeNMASigTermVerticaOp(computeHostsToStop, computeHostCatPathMap)
	if err != nil {
		return err
	}
	*instructions = append(*instructions,
		&nmaHealthOp,
		&nmaSigTermNodeOp,
	)
	return nil
}
