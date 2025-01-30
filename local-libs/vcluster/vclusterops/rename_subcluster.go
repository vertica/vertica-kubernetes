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

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type VRenameSubclusterOptions struct {
	// Basic db info
	DatabaseOptions
	// Name of the subcluster to rename
	SCName string
	// New name of the subcluster
	NewSCName string
	// Name of the sandbox
	// Use this option when renaming a subcluster in a sandbox.
	// if this option is not set, the subcluster will be renamed in the main cluster.
	Sandbox string
}

func VRenameSubclusterFactory() VRenameSubclusterOptions {
	options := VRenameSubclusterOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VRenameSubclusterOptions) validateEonOptions(_ vlog.Printer) error {
	if !options.IsEon {
		return fmt.Errorf("rename subcluster is only supported in Eon mode")
	}
	return nil
}

func (options *VRenameSubclusterOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateEonOptions(logger)
	if err != nil {
		return err
	}

	err = options.validateAuthOptions(RenameScCmd.CmdString(), logger)
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

	if options.NewSCName == "" {
		return fmt.Errorf("must specify a new subcluster name")
	}

	err = util.ValidateScName(options.NewSCName)
	if err != nil {
		return err
	}
	return options.validateBaseOptions(RenameScCmd, logger)
}

// analyzeOptions will modify some options based on what is chosen
func (options *VRenameSubclusterOptions) analyzeOptions() (err error) {
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

func (options *VRenameSubclusterOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VRenameSubcluster alter the name of the specified subcluster
func (vcc VClusterCommands) VRenameSubcluster(options *VRenameSubclusterOptions) error {
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

	// retrieve information from the database to accurately determine the state of each node in both the main cluster and sandbox
	vdb := makeVCoordinationDatabase()
	err = vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return err
	}

	// produce rename subcluster instructions
	instructions, err := vcc.produceRenameSubclusterInstructions(options, &vdb)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to rename subcluster: %w", runError)
	}

	return nil
}

// The generated instructions will later perform the following operations necessary
// for a successful promote/demote subcluster operation:
// - Rename subclusters using one of the up nodes
func (vcc VClusterCommands) produceRenameSubclusterInstructions(options *VRenameSubclusterOptions,
	vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp

	// need username for https operations
	err := options.setUsePassword(vcc.Log)
	if err != nil {
		return instructions, err
	}

	var noHosts = []string{} // We pass in no hosts so that this op picks an up node from the previous call.
	httpsRenameScOp, err := makeHTTPSRenameSubclusterOp(noHosts, options.usePassword,
		options.UserName, options.Password, options.SCName, options.NewSCName, options.Sandbox, vdb)
	if err != nil {
		return nil, err
	}
	instructions = append(instructions, &httpsRenameScOp)
	return instructions, nil
}
