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

type SubclusterType string

const (
	Primary   SubclusterType = "primary"
	Secondary SubclusterType = "secondary"
)

func (s SubclusterType) IsValid() bool {
	switch s {
	case Primary, Secondary:
		return true
	}
	return false
}

type VAlterSubclusterTypeOptions struct {
	// Basic db info
	DatabaseOptions
	// Name of the subcluster to promote or demote in sandbox or main cluster
	SCName string
	// Type of the subcluster to promote or demote
	// Set to primary to demote the subcluster.
	// Set to secondary to promote the subcluster.
	SCType SubclusterType
	// Name of the sandbox
	// Use this option when promoting or demoting a subcluster in a sandbox.
	// If this option is not set, the subcluster will be promoted or demoted in the main cluster.
	Sandbox string
}

func VPromoteDemoteFactory() VAlterSubclusterTypeOptions {
	options := VAlterSubclusterTypeOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VAlterSubclusterTypeOptions) validateEonOptions(_ vlog.Printer) error {
	if !options.IsEon {
		return fmt.Errorf("promote or demote subclusters are only supported in Eon mode")
	}
	return nil
}

func (options *VAlterSubclusterTypeOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateEonOptions(logger)
	if err != nil {
		return err
	}

	err = options.validateAuthOptions(AlterSubclusterTypeCmd.CmdString(), logger)
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

	if !options.SCType.IsValid() {
		return fmt.Errorf("invalid subcluster type: must be 'primary' or 'secondary'")
	}
	return options.validateBaseOptions(AlterSubclusterTypeCmd, logger)
}

// analyzeOptions will modify some options based on what is chosen
func (options *VAlterSubclusterTypeOptions) analyzeOptions() (err error) {
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}
	return nil
}

func (options *VAlterSubclusterTypeOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VAlterSubclusterType can promote/demote subcluster to different types
func (vcc VClusterCommands) VAlterSubclusterType(options *VAlterSubclusterTypeOptions) error {
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
	err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, options.Sandbox)
	if err != nil {
		return err
	}

	// produce alter subcluster type instructions
	instructions, err := vcc.produceAlterSubclusterTypeInstructions(options, &vdb)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		if options.SCType == Secondary {
			return fmt.Errorf("fail to promote subcluster: %w", runError)
		}
		if options.SCType == Primary {
			return fmt.Errorf("fail to demote subcluster: %w", runError)
		}
	}

	return nil
}

// The generated instructions will later perform the following operations necessary
// for a successful alter subcluster type operation:
//   - Promote subclusters using one of the up nodes in the main subcluster or a sandbox other than the target subcluster
//     and subcluster type is secondary
//   - Demote subclusters using one of the up nodes in the main subcluster or a sandbox other than the target subcluster
//     and subcluster type is primary
func (vcc VClusterCommands) produceAlterSubclusterTypeInstructions(options *VAlterSubclusterTypeOptions,
	vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp

	// need username for https operations
	err := options.setUsePassword(vcc.Log)
	if err != nil {
		return instructions, err
	}

	var noHosts = []string{} // We pass in no hosts so that this op picks an up node from the previous call.
	if options.SCType == Secondary {
		httpsPromoteScOp, err := makeHTTPSPromoteSubclusterOp(noHosts, options.usePassword,
			options.UserName, options.Password, options.SCName, options.Sandbox, vdb)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, &httpsPromoteScOp)
	} else if options.SCType == Primary {
		httpsDemoteScOp, err := makeHTTPSDemoteSubclusterOp(noHosts, options.usePassword,
			options.UserName, options.Password, options.SCName, options.Sandbox, vdb)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, &httpsDemoteScOp)
	} else {
		return nil, fmt.Errorf("failed to add instructions: unsupported subcluster type '%s'", options.SCType)
	}

	return instructions, nil
}
