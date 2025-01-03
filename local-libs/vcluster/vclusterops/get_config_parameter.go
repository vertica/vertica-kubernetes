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

type VGetConfigurationParameterOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	/* part 2: get configuration parameters options */
	Sandbox         string
	ConfigParameter string
	Level           string
}

func VGetConfigurationParameterOptionsFactory() VGetConfigurationParameterOptions {
	opt := VGetConfigurationParameterOptions{}
	// set default values to the params
	opt.setDefaultValues()

	return opt
}

func (opt *VGetConfigurationParameterOptions) validateParseOptions(logger vlog.Printer) error {
	err := opt.validateBaseOptions(GetConfigurationParameterCmd, logger)
	if err != nil {
		return err
	}

	err = opt.validateAuthOptions(GetConfigurationParameterCmd.CmdString(), logger)
	if err != nil {
		return err
	}

	return opt.validateExtraOptions(logger)
}

func (opt *VGetConfigurationParameterOptions) validateExtraOptions(logger vlog.Printer) error {
	if opt.ConfigParameter == "" {
		errStr := util.EmptyConfigParamErrMsg
		logger.PrintError(errStr)
		return errors.New(errStr)
	}
	// opt.Level could be empty (which means database level)
	return nil
}

func (opt *VGetConfigurationParameterOptions) analyzeOptions() (err error) {
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

func (opt *VGetConfigurationParameterOptions) validateAnalyzeOptions(log vlog.Printer) error {
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

// VGetConfigurationParameters gets the value of a database configuration parameter.
// It returns the parameter value as a string and any error encountered.
func (vcc VClusterCommands) VGetConfigurationParameters(options *VGetConfigurationParameterOptions) (string, error) {
	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return "", err
	}

	var retrievedParamValue string

	// produce get configuration parameters instructions
	instructions, err := vcc.produceGetConfigurationParameterInstructions(options, &retrievedParamValue)
	if err != nil {
		return "", fmt.Errorf("fail to produce instructions, %w", err)
	}

	// Create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return "", fmt.Errorf("fail to get configuration parameter: %w", runError)
	}

	return retrievedParamValue, nil
}

// The generated instructions will later perform the following operations necessary
// for a successful get configuration parameter action.
//   - Check NMA connectivity
//   - Check UP nodes and sandboxes info
//   - Send get configuration parameter request
func (vcc VClusterCommands) produceGetConfigurationParameterInstructions(
	options *VGetConfigurationParameterOptions, retrievedParamValue *string) ([]clusterOp, error) {
	var instructions []clusterOp

	assertMainClusterUpNodes := options.Sandbox == ""

	// get up hosts in all sandboxes/clusters
	// exit early if specified sandbox has no up hosts
	// up hosts will be filtered by sandbox name in prepare stage of nmaGetConfigurationParameterOp
	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesWithSandboxOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password,
		GetConfigurationParameterCmd, options.Sandbox, assertMainClusterUpNodes)
	if err != nil {
		return instructions, err
	}

	nmaHealthOp := makeNMAHealthOp(options.Hosts)

	nmaGetConfigOp, err := makeNMAGetConfigurationParameterOp(options.Hosts,
		options.UserName, options.DBName, options.Sandbox,
		options.ConfigParameter, options.Level, retrievedParamValue,
		options.Password, options.usePassword)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&httpsGetUpNodesOp,
		&nmaGetConfigOp,
	)

	return instructions, nil
}
