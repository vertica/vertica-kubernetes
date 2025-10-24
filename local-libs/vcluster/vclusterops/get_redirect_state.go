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

type VGetRedirectStateOptions struct {
	DatabaseOptions
	Sandbox    string
	ExcludeIDs []string
	result     []RedirectStateRow
}

func VGetRedirectStateOptionsFactory() VGetRedirectStateOptions {
	options := VGetRedirectStateOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VGetRedirectStateOptions) validateRequiredOptions(logger vlog.Printer) error {
	if err := options.validateBaseOptions(GetRedirectStateCmd, logger); err != nil {
		return err
	}
	return nil
}

func (options *VGetRedirectStateOptions) validateParseOptions(log vlog.Printer) error {
	// validate required parameters
	err := options.validateRequiredOptions(log)
	if err != nil {
		return err
	}

	err = options.validateAuthOptions(GetRedirectStateCmd.CmdString(), log)
	if err != nil {
		return err
	}
	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VGetRedirectStateOptions) analyzeOptions() (err error) {
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

func (options *VGetRedirectStateOptions) validateAnalyzeOptions(log vlog.Printer) error {
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

func (vcc VClusterCommands) VGetRedirectState(options *VGetRedirectStateOptions) ([]RedirectStateRow, error) {
	// validate and analyze options
	if err := options.validateAnalyzeOptions(vcc.Log); err != nil {
		return nil, err
	}

	instructions, err := vcc.produceGetRedirectStateInstructions(options)
	if err != nil {
		return nil, fmt.Errorf("fail to produce instructions for getting redirect state, %w", err)
	}
	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	if err = clusterOpEngine.run(vcc.Log); err != nil {
		return nil, fmt.Errorf("failed to get vertica redirect state: %w", err)
	}
	return options.result, nil
}

func (vcc *VClusterCommands) produceGetRedirectStateInstructions(options *VGetRedirectStateOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	nmaHealthOp := makeNMAHealthOp(options.Hosts)
	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesWithSandboxOp(options.DBName, options.Hosts, options.usePassword,
		options.UserName, options.Password, GetRedirectStateCmd, options.Sandbox, options.Sandbox == util.MainClusterSandbox)
	if err != nil {
		return instructions, err
	}
	op, err := makeNmaGetRedirectStateOp(options.Hosts, options.UserName, options.DBName, options.Sandbox,
		options.Password, options.usePassword, options.ExcludeIDs, &options.result)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &nmaHealthOp, &httpsGetUpNodesOp, &op)
	return instructions, nil
}
