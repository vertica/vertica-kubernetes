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
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type healthWatchdogSetOptions struct {
	ParameterName  string
	Action         string
	Value          string
	PolicySettings map[string]string
}

type VHealthWatchdogSetOptions struct {
	// Part 1: basic db info
	DatabaseOptions

	// Part 2: health watchdog set options
	healthWatchdogSetOptions
}

func VHealthWatchdogSetOptionsFactory() VHealthWatchdogSetOptions {
	options := VHealthWatchdogSetOptions{}
	options.setDefaultValues()
	return options
}

// Validate all options required for health watchdog set
func (options *VHealthWatchdogSetOptions) validateRequiredOptions(logger vlog.Printer) error {
	// Validate DB name, hosts, etc.
	err := options.validateBaseOptions(HealthWatchdogSetCmd, logger)
	if err != nil {
		return err
	}

	if options.Action == "" {
		return fmt.Errorf("action field is empty")
	}

	normalizedAction := strings.ToLower(options.Action)
	normalizedParameterName := strings.ToLower(options.ParameterName)

	switch normalizedAction {
	case "set_config_parameter":
		if err := options.validateSetConfigParameters(normalizedParameterName); err != nil {
			return err
		}

	case "clear_config_parameter":
		if err := options.validateClearConfigParameters(normalizedParameterName); err != nil {
			return err
		}

	case "set_health_watchdog_policy":
		if err := options.validateSetHealthWatchdogPolicyParameters(); err != nil {
			return err
		}
	}

	return nil
}

func (options *VHealthWatchdogSetOptions) validateSetConfigParameters(normalizedParameterName string) error {
	if normalizedParameterName == "" {
		return fmt.Errorf("field 'parameter_name' is empty for 'action': '%s'", "set_config_parameter")
	}
	if options.Value == "" {
		return fmt.Errorf("field 'value' is empty for 'action': '%s'", "set_config_parameter")
	}
	return nil
}

func (options *VHealthWatchdogSetOptions) validateClearConfigParameters(normalizedParameterName string) error {
	if normalizedParameterName == "" {
		return fmt.Errorf("field 'parameter_name' is empty for 'action': '%s'", "clear_config_parameter")
	}
	return nil
}

func (options *VHealthWatchdogSetOptions) validateSetHealthWatchdogPolicyParameters() error {
	if len(options.PolicySettings) == 0 {
		return fmt.Errorf("'policy_settings' is empty for 'action': '%s'", "set_health_watchdog_policy")
	}
	return nil
}

func (options *VHealthWatchdogSetOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VHealthWatchdogSetOptions) analyzeOptions() (err error) {
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

func (options *VHealthWatchdogSetOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VHealthWatchdogSet sets the value for a health watchdog parameter
func (vcc VClusterCommands) VHealthWatchdogSet(options *VHealthWatchdogSetOptions) error {
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

	instructions, err := vcc.produceHealthWatchdogSetInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add latest context to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	return runError
}

// The generated instructions will later perform the following operations necessary for health watchdog set
//   - Check NMA connectivity
//   - Make request to NMA to set a health watchdog value
func (vcc VClusterCommands) produceHealthWatchdogSetInstructions(options *VHealthWatchdogSetOptions) ([]clusterOp,
	error) {
	var instructions []clusterOp
	vdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return instructions, err
	}

	// Get up hosts - health watchdog set request will be sent to all up hosts
	hosts := options.Hosts
	hosts = vdb.filterUpHostListBySandbox(hosts, util.MainClusterSandbox)
	if len(hosts) == 0 {
		return instructions, fmt.Errorf("found no UP nodes for health watchdog set")
	}

	// Get initiator host to send NMA requests to
	initiator, err := getInitiatorHost(vdb.PrimaryUpNodes, []string{} /* skip hosts */)
	if err != nil {
		return instructions, err
	}
	initiatorHost := []string{initiator}

	nmaHealthOp := makeNMAHealthOp(initiatorHost)

	nmaHealthWatchdogSetData := nmaHealthWatchdogSetData{}
	nmaHealthWatchdogSetData.DBName = options.DBName
	nmaHealthWatchdogSetData.UserName = options.UserName
	nmaHealthWatchdogSetData.Password = options.Password
	nmaHealthWatchdogSetData.ParameterName = options.ParameterName
	nmaHealthWatchdogSetData.Action = options.Action
	nmaHealthWatchdogSetData.Value = options.Value
	nmaHealthWatchdogSetData.PolicySettings = options.PolicySettings

	nmaHealthWatchdogSetOp, err := makeHealthWatchdogSetOp(initiatorHost, options.usePassword,
		&nmaHealthWatchdogSetData)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaHealthWatchdogSetOp,
	)

	return instructions, nil
}
