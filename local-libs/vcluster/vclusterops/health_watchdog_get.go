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
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type healthWatchdogGetOptions struct {
	ParameterName string
	Action        string
}

type VHealthWatchdogGetOptions struct {
	// Part 1: basic db info
	DatabaseOptions

	// Part 2: health watchdog get options
	healthWatchdogGetOptions
}

type HealthWatchdogValue map[string]any

type HealthWatchdogHostValues struct {
	Host   string
	Values []HealthWatchdogValue
}

func VHealthWatchdogGetValueOptionsFactory() VHealthWatchdogGetOptions {
	options := VHealthWatchdogGetOptions{}
	options.setDefaultValues()
	return options
}

// Validate all options required for health watchdog get
func (options *VHealthWatchdogGetOptions) validateRequiredOptions(logger vlog.Printer) error {
	// Validate DB name, hosts, etc.
	err := options.validateBaseOptions(HealthWatchdogGetCmd, logger)
	if err != nil {
		return err
	}

	if options.Action == "" {
		return fmt.Errorf("action field is empty")
	}

	return nil
}

func (options *VHealthWatchdogGetOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VHealthWatchdogGetOptions) analyzeOptions() (err error) {
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

func (options *VHealthWatchdogGetOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VHealthWatchdogGet gets the value for a health watchdog parameter
func (vcc VClusterCommands) VHealthWatchdogGet(options *VHealthWatchdogGetOptions) (*[]HealthWatchdogHostValues,
	error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */
	results := []HealthWatchdogHostValues{}

	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return nil, err
	}

	instructions, err := vcc.produceHealthWatchdogGetInstructions(options, &results)
	if err != nil {
		return nil, fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add latest context to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)

	// Check if the engine returned back-off error.
	var tlsErr *util.ErrTLSBackedOff

	// Checks if runError is of type ErrTLSBackedOff.
	if errors.As(runError, &tlsErr) {
		return nil, tlsErr
	}

	// For any other error.
	if runError != nil {
		return nil, fmt.Errorf("fail to get health watchdog values: %w", runError)
	}
	return &results, runError
}

// The generated instructions will later perform the following operations necessary for health watchdog get
//   - Check NMA connectivity
//   - Make request to NMA to get a health watchdog value
func (vcc VClusterCommands) produceHealthWatchdogGetInstructions(options *VHealthWatchdogGetOptions,
	healthWatchdogValues *[]HealthWatchdogHostValues) ([]clusterOp, error) {
	var instructions []clusterOp
	vdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return instructions, err
	}

	// Get up hosts
	hosts := options.Hosts
	hosts = vdb.filterUpHostListBySandbox(hosts, util.MainClusterSandbox)
	if len(hosts) == 0 {
		return instructions, fmt.Errorf("found no UP nodes for health watchdog get")
	}

	// When checking cluster health, the request only needs to be sent to one initiator host
	if options.Action == "check_cluster_health" {
		initiator, err := getInitiatorHost(vdb.PrimaryUpNodes, []string{} /* skip hosts */)
		if err != nil {
			return instructions, err
		}
		hosts = []string{initiator}
	}

	nmaHealthOp := makeNMAHealthOp(hosts)

	nmaHealthWatchdogGetData := nmaHealthWatchdogGetData{}
	nmaHealthWatchdogGetData.DBName = options.DBName
	nmaHealthWatchdogGetData.UserName = options.UserName
	nmaHealthWatchdogGetData.Password = options.Password
	nmaHealthWatchdogGetData.ParameterName = options.ParameterName
	nmaHealthWatchdogGetData.Action = options.Action

	nmaHealthWatchdogGetOp, err := makeHealthWatchdogGetOp(hosts, options.usePassword,
		&nmaHealthWatchdogGetData, healthWatchdogValues, vdb.HostNodeMap, vcc.Log)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaHealthWatchdogGetOp,
	)

	return instructions, nil
}
