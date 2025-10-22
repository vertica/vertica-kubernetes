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

type VRestartNMAOptions struct {
	DatabaseOptions
	Sandbox        string
	PollingTimeout int
}

func VRestartNMAOptionsFactory() VRestartNMAOptions {
	options := VRestartNMAOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VRestartNMAOptions) validateRequiredOptions(logger vlog.Printer) error {
	if err := options.validateBaseOptions(RestartNMACmd, logger); err != nil {
		return err
	}
	return nil
}

func (options *VRestartNMAOptions) validateParseOptions(log vlog.Printer) error {
	// validate required parameters
	err := options.validateRequiredOptions(log)
	if err != nil {
		return err
	}

	err = options.validateAuthOptions(RestartNMACmd.CmdString(), log)
	if err != nil {
		return err
	}
	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VRestartNMAOptions) analyzeOptions() (err error) {
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

func (options *VRestartNMAOptions) validateAnalyzeOptions(log vlog.Printer) error {
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

func (vcc VClusterCommands) VRestartNMA(options *VRestartNMAOptions) error {
	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	instructions, err := vcc.produceRestartNMAInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions to restart NMA, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to restart NMA: %w", runError)
	}
	return nil
}

func (vcc *VClusterCommands) produceRestartNMAInstructions(options *VRestartNMAOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	vdb := makeVCoordinationDatabase()
	if err := vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions); err != nil {
		return instructions, err
	}
	vdb.filterSandboxNodes(options.Sandbox)
	restartOp := makeNMARestartOp(vdb.HostList)
	// it's called "cert health" but still works for this purpose
	pollNmaOp := makeNMAPollCertHealthOp(vdb.HostList, options.PollingTimeout)
	instructions = append(instructions, &restartOp, &pollNmaOp)

	return instructions, nil
}
