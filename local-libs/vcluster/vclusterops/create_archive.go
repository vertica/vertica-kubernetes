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

const CreateArchiveDefaultNumRestore = 0

type VCreateArchiveOptions struct {
	DatabaseOptions

	// Required arguments
	ArchiveName string
	// Optional arguments
	NumRestorePoint int
	Sandbox         string
}

func VCreateArchiveFactory() VCreateArchiveOptions {
	options := VCreateArchiveOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VCreateArchiveOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VCreateArchiveOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateEonOptions(logger)
	if err != nil {
		return err
	}
	err = options.validateBaseOptions(CreateArchiveCmd, logger)
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

func (options *VCreateArchiveOptions) validateExtraOptions() error {
	if options.NumRestorePoint < 0 {
		return fmt.Errorf("number of restore points must greater than 0")
	}
	if options.Sandbox != "" {
		return util.ValidateSandboxName(options.Sandbox)
	}
	return nil
}

func (options *VCreateArchiveOptions) validateEonOptions(_ vlog.Printer) error {
	if !options.IsEon {
		return fmt.Errorf("create archive is only supported in Eon mode")
	}
	return nil
}

func (options *VCreateArchiveOptions) validateParseOptions(log vlog.Printer) error {
	// validate required parameters
	err := options.validateRequiredOptions(log)
	if err != nil {
		return err
	}

	err = options.validateEonOptions(log)
	if err != nil {
		return err
	}

	err = options.validateAuthOptions(CreateArchiveCmd.CmdString(), log)
	if err != nil {
		return err
	}

	// validate all other params
	err = options.validateExtraOptions()
	if err != nil {
		return err
	}
	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VCreateArchiveOptions) analyzeOptions() (err error) {
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

func (options *VCreateArchiveOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := options.validateParseOptions(log); err != nil {
		return err
	}
	if err := options.analyzeOptions(); err != nil {
		return err
	}
	if err := options.setUsePassword(log); err != nil {
		return err
	}
	return options.validateUserName(log)
}

func (vcc VClusterCommands) VCreateArchive(options *VCreateArchiveOptions) error {
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

	// produce create acchive instructions
	instructions, err := vcc.produceCreateArchiveInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to create archive: %w", runError)
	}
	return nil
}

// The generated instructions will later perform the following operations necessary
// for a successful create_archive:
//   - Retrieve VDB from HTTP endpoints
//   - Run create archive query
func (vcc *VClusterCommands) produceCreateArchiveInstructions(options *VCreateArchiveOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	vdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, util.MainClusterSandbox)
	if err != nil {
		return instructions, err
	}
	// get up hosts
	hosts := options.Hosts
	// Trim host list
	hosts = vdb.filterUpHostlist(hosts, options.Sandbox)
	bootstrapHost := []string{getInitiator(hosts)}

	httpsCreateArchiveOp, err := makeHTTPSCreateArchiveOp(bootstrapHost, options.usePassword,
		options.UserName, options.Password, options.ArchiveName, options.NumRestorePoint)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsCreateArchiveOp)
	return instructions, nil
}
