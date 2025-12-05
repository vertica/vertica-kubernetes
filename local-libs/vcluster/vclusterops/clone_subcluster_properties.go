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

type VCloneSubclusterPropertiesOptions struct {
	DatabaseOptions
	SourceSubcluster string
	TargetSubcluster string
}

func VCloneSubclusterPropertiesOptionsFactory() VCloneSubclusterPropertiesOptions {
	options := VCloneSubclusterPropertiesOptions{}
	options.setDefaultValues()
	return options
}

func (options *VCloneSubclusterPropertiesOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VCloneSubclusterPropertiesOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(CloneSubclusterPropertiesCmd, logger)
	if err != nil {
		return err
	}

	if options.SourceSubcluster == "" {
		return fmt.Errorf("must specify a source subcluster name")
	}

	if options.TargetSubcluster == "" {
		return fmt.Errorf("must specify a target subcluster name")
	}

	if options.SourceSubcluster == options.TargetSubcluster {
		return fmt.Errorf("source and target subclusters cannot be the same")
	}

	return nil
}

func (options *VCloneSubclusterPropertiesOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VCloneSubclusterPropertiesOptions) analyzeOptions() error {
	// Resolve hostnames if needed
	if len(options.RawHosts) > 0 {
		hosts, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hosts
	}
	return nil
}

func (options *VCloneSubclusterPropertiesOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VCloneSubclusterProperties clones properties from source to target subcluster.
// This operation copies configuration settings, resource pool settings, and other
// subcluster-level properties from the source to the target subcluster.
func (vcc VClusterCommands) VCloneSubclusterProperties(
	options *VCloneSubclusterPropertiesOptions,
) error {
	/*
	 *   - Validate Options
	 *   - Get VDB (database info and hosts)
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	err := options.validateUserName(vcc.Log)
	if err != nil {
		return err
	}

	// Validate and analyze options
	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// Get VDB to obtain database info and host list
	vdb := makeVCoordinationDatabase()
	err = vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return fmt.Errorf("failed to get database info: %w", err)
	}

	if len(vdb.HostList) == 0 {
		return fmt.Errorf("no hosts available to execute clone operation")
	}

	// Produce instructions
	instructions, err := vcc.produceCloneSubclusterPropertiesInstructions(options, &vdb)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// Create a VClusterOpEngine
	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)

	// Execute the instructions
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to clone properties from %s to %s, %w",
			options.SourceSubcluster, options.TargetSubcluster, runError)
	}

	return nil
}

// produceCloneSubclusterPropertiesInstructions builds a list of instructions to execute
// for cloning subcluster properties.
//
// The generated instructions will:
//   - Get UP nodes from the database
//   - Validate that the source subcluster exists (fail-fast with clear error)
//   - Execute clone_subcluster_properties SQL function via NMA
func (vcc *VClusterCommands) produceCloneSubclusterPropertiesInstructions(
	options *VCloneSubclusterPropertiesOptions,
	vdb *VCoordinationDatabase,
) ([]clusterOp, error) {
	var instructions []clusterOp

	// Get UP nodes - needed for validation operations
	mainCluster := true
	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesWithSandboxOp(options.DBName, vdb.HostList, options.usePassword, options.UserName,
		options.Password, CloneSubclusterPropertiesCmd, util.MainClusterSandbox, mainCluster)
	if err != nil {
		return instructions, err
	}

	// Validate source subcluster exists
	validateSourceOp, err := makeHTTPSGetSubclusterInfoOp(options.usePassword, options.UserName, options.Password,
		options.SourceSubcluster, CloneSubclusterPropertiesCmd)
	if err != nil {
		return instructions, err
	}

	// Clone properties via NMA
	cloneOp, err := makeNMACloneSubclusterPropertiesOp(vdb.HostList, options.DBName, options.UserName, options.Password,
		options.SourceSubcluster, options.TargetSubcluster)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsGetUpNodesOp,
		&validateSourceOp,
		&cloneOp,
	)

	return instructions, nil
}
