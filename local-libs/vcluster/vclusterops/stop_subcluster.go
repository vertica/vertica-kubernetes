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

type VStopSubclusterOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	/* part 2: eon db info */
	DrainSeconds int    // time in seconds to wait for subcluster users' disconnection, its default value is 60
	SCName       string // subcluster name
	Force        bool   // force the subcluster to shutdown immediately even if users are connected
}

func VStopSubclusterOptionsFactory() VStopSubclusterOptions {
	options := VStopSubclusterOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VStopSubclusterOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
	options.DrainSeconds = util.DefaultDrainSeconds
}

func (options *VStopSubclusterOptions) validateRequiredOptions(log vlog.Printer) error {
	err := options.validateBaseOptions(StopSubclusterCmd, log)
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

func (options *VStopSubclusterOptions) validateEonOptions(log vlog.Printer) error {
	if !options.IsEon {
		return fmt.Errorf("stop subcluster is only supported in Eon mode")
	}
	if options.Force {
		// this log is for vclusterops user since they probably set both DrainSeconds and Force
		log.Info("The subcluster will be forcibly shutdown so provided drain seconds will be ignored")
	}

	return nil
}

func (options *VStopSubclusterOptions) validateExtraOptions() error {
	return nil
}

func (options *VStopSubclusterOptions) validateParseOptions(log vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(log)
	if err != nil {
		return err
	}
	// batch 2: validate eon params
	err = options.validateEonOptions(log)
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

// resolve hostnames to be IPs
func (options *VStopSubclusterOptions) analyzeOptions() (err error) {
	// resolve RawHosts to be IP addresses
	if len(options.RawHosts) > 0 {
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VStopSubclusterOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := options.validateParseOptions(log); err != nil {
		return err
	}
	return options.analyzeOptions()
}

func (vcc VClusterCommands) VStopSubcluster(options *VStopSubclusterOptions) error {
	/*
	 *   - Validate Options
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	instructions, err := vcc.produceStopSCInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to production instructions: %w", err)
	}

	// Create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("failed to stop subcluster %s: %w", options.SCName, runError)
	}

	return nil
}

// produceStopSCInstructions will build a list of instructions to execute for
// the stop subcluster operation.
//
// The generated instructions will later perform the following operations necessary
// for a successful stop_subcluster:
//   - Get up nodes in the target subcluster through https call
//   - Sync catalog through the first up node in the target subcluster
//   - Stop subcluster through the first up node in the target subcluster
//   - Check if there are any running nodes in the target subcluster
func (vcc *VClusterCommands) produceStopSCInstructions(options *VStopSubclusterOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	// when password is specified, we will use username/password to call https endpoints
	usePassword := false
	if options.Password != nil {
		usePassword = true
		err := options.validateUserName(vcc.Log)
		if err != nil {
			return instructions, err
		}
	}

	httpsGetSubclusterInfoOp, err := makeHTTPSGetSubclusterInfoOp(usePassword, options.UserName, options.Password,
		options.SCName, StopSubclusterCmd)
	if err != nil {
		return instructions, err
	}

	httpsGetUpNodesOp, err := makeHTTPSGetUpScNodesOp(options.DBName, options.Hosts,
		usePassword, options.UserName, options.Password, StopSubclusterCmd, options.SCName)
	if err != nil {
		return instructions, err
	}

	httpsSyncCatalogOp, err := makeHTTPSSyncCatalogOpWithoutHosts(usePassword, options.UserName, options.Password, StopSCSyncCat)
	if err != nil {
		return instructions, err
	}

	httpsStopSCOp, err := makeHTTPSStopSCOp(usePassword, options.UserName, options.Password,
		options.SCName, options.DrainSeconds, options.Force)
	if err != nil {
		return instructions, err
	}

	httpsCheckDBRunningOp, err := makeHTTPSCheckRunningDBOpWithoutHosts(usePassword, options.UserName, options.Password, StopSC)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsGetUpNodesOp,
		&httpsGetSubclusterInfoOp,
		&httpsSyncCatalogOp,
		&httpsStopSCOp,
		&httpsCheckDBRunningOp,
	)

	return instructions, nil
}
