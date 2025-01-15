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
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type VSetConfigurationParameterOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	/* part 2: set configuration parameters options */
	Sandbox         string
	ConfigParameter string
	// set value literally to "null" to clear the value of a config parameter
	Value string
	Level string
}

func VSetConfigurationParameterOptionsFactory() VSetConfigurationParameterOptions {
	opt := VSetConfigurationParameterOptions{}
	// set default values to the params
	opt.setDefaultValues()

	return opt
}

func (opt *VSetConfigurationParameterOptions) validateParseOptions(logger vlog.Printer) error {
	err := opt.validateBaseOptions(SetConfigurationParameterCmd, logger)
	if err != nil {
		return err
	}

	err = opt.validateAuthOptions(SetConfigurationParameterCmd.CmdString(), logger)
	if err != nil {
		return err
	}

	return opt.validateExtraOptions(logger)
}

func (opt *VSetConfigurationParameterOptions) validateExtraOptions(logger vlog.Printer) error {
	if opt.ConfigParameter == "" {
		errStr := util.EmptyConfigParamErrMsg
		logger.PrintError(errStr)
		return errors.New(errStr)
	}
	// opt.Value could be empty (which is not equivalent to "null")
	// opt.Level could be empty (which means database level)
	return nil
}

func (opt *VSetConfigurationParameterOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(opt.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		opt.Hosts, err = util.ResolveRawHostsToAddresses(opt.RawHosts, opt.IPv6)
		if err != nil {
			return err
		}
		opt.normalizePaths()
	}
	return nil
}

func (opt *VSetConfigurationParameterOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := opt.validateParseOptions(log); err != nil {
		return err
	}
	if err := opt.analyzeOptions(); err != nil {
		return err
	}
	if err := opt.setUsePassword(log); err != nil {
		return err
	}
	// username is always required when local db connection is made
	return opt.validateUserName(log)
}

// VSetConfigurationParameters sets or clears the value of a database configuration parameter.
// It returns any error encountered.
func (vcc VClusterCommands) VSetConfigurationParameters(options *VSetConfigurationParameterOptions) error {
	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// produce set configuration parameters instructions
	instructions, err := vcc.produceSetConfigurationParameterInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// Create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to set configuration parameter: %w", runError)
	}

	return nil
}

// The generated instructions will later perform the following operations necessary
// for a successful set configuration parameter action.
//   - Check NMA connectivity
//   - Check UP nodes and sandboxes info
//   - Send set configuration parameter request
func (vcc VClusterCommands) produceSetConfigurationParameterInstructions(
	options *VSetConfigurationParameterOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	assertMainClusterUpNodes := options.Sandbox == ""

	// get up hosts in all sandboxes/clusters
	// exit early if specified sandbox has no up hosts
	// up hosts will be filtered by sandbox name in prepare stage of nmaSetConfigurationParameterOp
	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesWithSandboxOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password,
		SetConfigurationParameterCmd, options.Sandbox, assertMainClusterUpNodes)
	if err != nil {
		return instructions, err
	}

	nmaHealthOp := makeNMAHealthOp(options.Hosts)

	nmaSetConfigOp, err := makeNMASetConfigurationParameterOp(options.Hosts,
		options.UserName, options.DBName, options.Sandbox,
		options.ConfigParameter, options.Value, options.Level,
		options.Password, options.usePassword)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&httpsGetUpNodesOp,
		&nmaSetConfigOp,
	)

	return instructions, nil
}
