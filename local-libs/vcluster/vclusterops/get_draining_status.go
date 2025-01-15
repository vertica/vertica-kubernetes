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

type DrainingStatus struct {
	SubclusterName string `json:"subcluster_name"`
	Status         string `json:"drain_status"`
	RedirectTo     string `json:"redirect_to"`
}

type DrainingStatusList struct {
	StatusList []DrainingStatus `json:"draining_status_list"`
}

type VGetDrainingStatusOptions struct {
	// basic db info
	DatabaseOptions

	// the name of the sandbox to target, if left empty the main cluster is assumed
	Sandbox string
}

func VGetDrainingStatusFactory() VGetDrainingStatusOptions {
	opt := VGetDrainingStatusOptions{}
	// set default values to the params
	opt.setDefaultValues()

	return opt
}

func (opt *VGetDrainingStatusOptions) validateEonOptions(_ vlog.Printer) error {
	if !opt.IsEon {
		return fmt.Errorf("getting draining status is only supported in Eon mode")
	}
	return nil
}

func (opt *VGetDrainingStatusOptions) validateParseOptions(logger vlog.Printer) error {
	err := opt.validateEonOptions(logger)
	if err != nil {
		return err
	}

	err = opt.validateBaseOptions(GetDrainingStatusCmd, logger)
	if err != nil {
		return err
	}

	err = opt.validateAuthOptions(GetDrainingStatusCmd.CmdString(), logger)
	if err != nil {
		return err
	}

	return opt.validateExtraOptions()
}

func (opt *VGetDrainingStatusOptions) validateExtraOptions() error {
	if opt.Sandbox != "" {
		return util.ValidateSandboxName(opt.Sandbox)
	}
	return nil
}

func (opt *VGetDrainingStatusOptions) analyzeOptions() (err error) {
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

func (opt *VGetDrainingStatusOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := opt.validateParseOptions(log); err != nil {
		return err
	}
	if err := opt.analyzeOptions(); err != nil {
		return err
	}
	if err := opt.setUsePassword(log); err != nil {
		return err
	}
	return opt.validateUserName(log)
}

// VGetDrainingStatus retrieves draining status of subclusters in the main cluster or a sandbox
func (vcc VClusterCommands) VGetDrainingStatus(options *VGetDrainingStatusOptions) (dsList DrainingStatusList, err error) {
	// validate and analyze all options
	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return dsList, err
	}

	// produce get-draining-status instructions
	instructions, err := vcc.produceGetDrainingStatusInstructions(options, &dsList)
	if err != nil {
		return dsList, fmt.Errorf("fail to produce instructions, %w", err)
	}

	// Create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return dsList, fmt.Errorf("fail to get draining status: %w", runError)
	}

	return dsList, nil
}

// The generated instructions will later perform the following operations necessary
// for getting draining status of a cluster.
//   - Get up hosts of target sandbox
//   - Send get-draining-status request on the up hosts
func (vcc VClusterCommands) produceGetDrainingStatusInstructions(
	options *VGetDrainingStatusOptions, drainingStatusList *DrainingStatusList) ([]clusterOp, error) {
	var instructions []clusterOp

	assertMainClusterUpNodes := options.Sandbox == ""

	// get up hosts in all sandboxes/clusters
	// exit early if specified sandbox has no up hosts
	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesWithSandboxOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password,
		GetDrainingStatusCmd, options.Sandbox, assertMainClusterUpNodes)
	if err != nil {
		return instructions, err
	}

	httpsGetDrainingStatusOp, err := makeHTTPSGetDrainingStatusOp(options.usePassword,
		options.Sandbox, options.UserName, options.Password, drainingStatusList)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsGetUpNodesOp,
		&httpsGetDrainingStatusOp,
	)

	return instructions, nil
}
