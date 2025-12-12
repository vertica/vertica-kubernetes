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
)

// VSetRestartPolicyOptions set the restart policy options for a database
type VSetRestartPolicyOptions struct {
	DatabaseOptions
	Policy      string
	SandboxName string
}

func VSetRestartPolicyOptionsFactory() VSetRestartPolicyOptions {
	options := VSetRestartPolicyOptions{}
	// set default values to the params
	options.setDefaultValues()
	options.Policy = util.DefaultRestartPolicy

	return options
}

// analyzeOptions verifies the host options for the VSetRestartPolicyOptions struct and
// returns any error encountered.
func (options *VSetRestartPolicyOptions) analyzeOptions() error {
	if len(options.RawHosts) > 0 {
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}

	if !util.StringInArray(options.Policy, util.RestartPolicyList) {
		return fmt.Errorf("policy must be one of %v", util.RestartPolicyList)
	}

	return nil
}

func (options *VSetRestartPolicyOptions) validateParseOptions() error {
	if options.DBName == "" {
		return fmt.Errorf("database name must be provided")
	}

	err := util.ValidateDBName(options.DBName)
	if err != nil {
		return err
	}
	return nil
}

func (options *VSetRestartPolicyOptions) validateAnalyzeOptions() error {
	err := options.validateParseOptions()
	if err != nil {
		return err
	}

	return options.analyzeOptions()
}

func (vcc VClusterCommands) VSetRestartPolicy(options *VSetRestartPolicyOptions) error {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	err := options.validateAnalyzeOptions()
	if err != nil {
		return err
	}

	var vdb VCoordinationDatabase
	err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, AnySandbox)
	if err != nil {
		return err
	}

	instructions, err := vcc.produceSetRestartPolicyInstructions(options, &vdb)
	if err != nil {
		return err
	}

	// create a VClusterOpEngine for pre-check, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("failed to set restart policy: %w", runError)
	}

	return nil
}

func (vcc VClusterCommands) produceSetRestartPolicyInstructions(
	options *VSetRestartPolicyOptions,
	vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp

	// filter up hosts in the specified sandbox
	upHosts := vdb.filterUpHostListBySandbox(vdb.HostList, options.SandboxName)
	if len(upHosts) == 0 {
		return nil, fmt.Errorf("no up hosts found in the sandbox %q", options.SandboxName)
	}

	setRestartPolicyOp, err := makeNMASetRestartPolicyOp(upHosts,
		options.UserName, options.DBName, options.Password, options.Policy)
	if err != nil {
		return nil, err
	}

	instructions = append(instructions, &setRestartPolicyOp)

	return instructions, nil
}
