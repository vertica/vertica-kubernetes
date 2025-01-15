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

type VPollSubclusterStateOptions struct {
	DatabaseOptions

	SkipOptionsValidation bool
	SCName                string
	Timeout               int // timeout for polling, 0 means default
}

func VPollSubclusterStateOptionsFactory() VPollSubclusterStateOptions {
	options := VPollSubclusterStateOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VPollSubclusterStateOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VPollSubclusterStateOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(PollSubclusterStateCmd, logger)
	if err != nil {
		return err
	}

	return nil
}

func (options *VPollSubclusterStateOptions) analyzeOptions() (err error) {
	// resolve RawHosts to be IP addresses
	if len(options.RawHosts) > 0 {
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VPollSubclusterStateOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if options.SkipOptionsValidation {
		return nil
	}
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VPollSubclusterState waits for the given nodes to be up or down
func (vcc VClusterCommands) VPollSubclusterState(options *VPollSubclusterStateOptions) error {
	/*
	 *   - Validate Options
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	vcc.Log.V(0).Info("VPollSubclusterState method called", "options", options)
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	instructions, err := vcc.producePollSubclusterStateInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions: %w", err)
	}

	clusterOpEngine := makeClusterOpEngine(instructions, options)

	err = clusterOpEngine.run(vcc.Log)
	if err != nil {
		return fmt.Errorf("failed to poll for host status %v: %w", options.Hosts, err)
	}

	return nil
}

// producePollSubclusterStateInstructions will build a list of instructions to execute
//
// The generated instructions will later perform the following operations:
//   - Poll for the subcluster hosts to be all UP or DOWN
func (vcc *VClusterCommands) producePollSubclusterStateInstructions(options *VPollSubclusterStateOptions,
) (instructions []clusterOp, err error) {
	// when password is specified, we will use username/password to call https endpoints
	usePassword := false
	if options.Password != nil {
		usePassword = true
		if err = options.validateUserName(vcc.Log); err != nil {
			return
		}
	}

	httpsPollSubclusterNodeOp, err := makeHTTPSPollSubclusterNodeStateUpOp(options.Hosts, options.SCName, options.Timeout,
		usePassword, options.UserName, options.Password)
	if err != nil {
		return
	}

	instructions = append(instructions, &httpsPollSubclusterNodeOp)
	return
}
