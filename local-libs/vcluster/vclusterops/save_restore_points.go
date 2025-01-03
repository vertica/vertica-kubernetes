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

type VSaveRestorePointOptions struct {
	DatabaseOptions
	ArchiveName string

	// the name of the sandbox to target, if left empty the main cluster is assumed
	Sandbox string
}

func VSaveRestorePointFactory() VSaveRestorePointOptions {
	options := VSaveRestorePointOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VSaveRestorePointOptions) validateEonOptions(_ vlog.Printer) error {
	if !options.IsEon {
		return fmt.Errorf("save restore point is only supported in Eon mode")
	}
	return nil
}

// Save restore impl
func (options *VSaveRestorePointOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateEonOptions(logger)
	if err != nil {
		return err
	}
	err = options.validateBaseOptions(SaveRestorePointsCmd, logger)
	if err != nil {
		return err
	}
	if options.ArchiveName == "" {
		return fmt.Errorf("must specify an archive name")
	}
	err = util.ValidateArchiveName(options.ArchiveName)
	if err != nil {
		return err
	}
	return nil
}

func (options *VSaveRestorePointOptions) validateExtraOptions() error {
	if options.Sandbox != "" {
		return util.ValidateSandboxName(options.Sandbox)
	}
	return nil
}

func (options *VSaveRestorePointOptions) validateParseOptions(logger vlog.Printer) error {
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
func (options *VSaveRestorePointOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}
	return nil
}

func (options *VSaveRestorePointOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	if err := options.validateUserName(logger); err != nil {
		return err
	}
	if err := options.setUsePassword(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VSaveRestorePoint can save restore point to a given archive
func (vcc VClusterCommands) VSaveRestorePoint(options *VSaveRestorePointOptions) (err error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// validate and analyze options
	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// produce save restore points instructions
	instructions, err := vcc.produceSaveRestorePointsInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to save restore point: %w", runError)
	}
	return nil
}

// The generated instructions will later perform the following operations necessary
// for a successful save_restore_point:
//   - Retrieve VDB from HTTP endpoints
//   - Check NMA connectivity
//   - Run save restore points on the target node
func (vcc VClusterCommands) produceSaveRestorePointsInstructions(options *VSaveRestorePointOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	vdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, util.MainClusterSandbox)
	if err != nil {
		return instructions, err
	}

	// get up hosts
	hosts := options.Hosts
	nmaHealthOp := makeNMAHealthOp(options.Hosts)
	// Trim host list
	hosts = vdb.filterUpHostlist(hosts, options.Sandbox)
	bootstrapHost := []string{getInitiator(hosts)}

	requestData := saveRestorePointsRequestData{}
	requestData.ArchiveName = options.ArchiveName
	requestData.DBName = options.DBName
	requestData.UserName = options.UserName
	requestData.Password = options.Password

	nmaSaveRestorePointOp, err := makeNMASaveRestorePointsOp(vcc.Log, bootstrapHost,
		&requestData, options.Sandbox, options.usePassword)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions,
		&nmaHealthOp,
		&nmaSaveRestorePointOp)
	return instructions, nil
}
