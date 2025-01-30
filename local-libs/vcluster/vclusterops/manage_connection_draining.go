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

const (
	ActionPause     ConnectionDrainingAction = "pause"
	ActionRedirect  ConnectionDrainingAction = "redirect"
	ActionResume    ConnectionDrainingAction = "resume"
	hostRedirectMsg                          = "hostname to redirect to must not be empty when manage connection draining action is %q"
)

type ConnectionDrainingAction string

type VManageConnectionDrainingOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	/* part 2: manage connection draining options */
	// the client management action to be performed: pause, redirect, or resume
	Action ConnectionDrainingAction

	// the name of the sandbox to target, if left empty the default cluster is assumed
	Sandbox string

	// the subcluster to which designated client connection management action will
	// be performed, if empty all subclusters will be implied
	SCName string

	// the hostname to redirect client connections to, only used when action is redirect
	RedirectHostname string
}

func VManageConnectionDrainingOptionsFactory() VManageConnectionDrainingOptions {
	opt := VManageConnectionDrainingOptions{}
	// set default values to the params
	opt.setDefaultValues()

	return opt
}

func (opt *VManageConnectionDrainingOptions) validateEonOptions(_ vlog.Printer) error {
	if !opt.IsEon {
		return fmt.Errorf("manage connections is only supported in Eon mode")
	}
	return nil
}

func (opt *VManageConnectionDrainingOptions) validateParseOptions(logger vlog.Printer) error {
	err := opt.validateEonOptions(logger)
	if err != nil {
		return err
	}

	err = opt.validateBaseOptions(ManageConnectionDrainingCmd, logger)
	if err != nil {
		return err
	}

	err = opt.validateAuthOptions(ManageConnectionDrainingCmd.CmdString(), logger)
	if err != nil {
		return err
	}

	return opt.validateExtraOptions(logger)
}

func (opt *VManageConnectionDrainingOptions) validateExtraOptions(logger vlog.Printer) error {
	if opt.Action != ActionPause &&
		opt.Action != ActionRedirect &&
		opt.Action != ActionResume {
		logger.PrintError("manage connection draining action %q is invalid, must be one of"+
			" %q, %q, or %q", opt.Action, ActionPause, ActionRedirect, ActionResume)
		return fmt.Errorf("manage connection draining action %q is invalid", opt.Action)
	}
	if opt.Action == ActionRedirect {
		if opt.RedirectHostname == "" {
			logger.PrintError(hostRedirectMsg, ActionRedirect)
			return fmt.Errorf(hostRedirectMsg, ActionRedirect)
		}
	}
	return nil
}

func (opt *VManageConnectionDrainingOptions) analyzeOptions() (err error) {
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

func (opt *VManageConnectionDrainingOptions) validateAnalyzeOptions(log vlog.Printer) error {
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

// VManageConnectionDraining manages connection draining of nodes by pausing, redirecting, or
// resuming connections. It returns any error encountered.
func (vcc VClusterCommands) VManageConnectionDraining(options *VManageConnectionDrainingOptions) error {
	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// produce manage connection draining instructions
	instructions, err := vcc.produceManageConnectionDrainingInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// Create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to %v connections: %w", options.Action, runError)
	}

	return nil
}

// The generated instructions will later perform the following operations necessary
// for a successful manage connection draining action.
//   - Check NMA connectivity
//   - Check UP nodes and sandboxes info
//   - Send manage connection draining request
func (vcc VClusterCommands) produceManageConnectionDrainingInstructions(
	options *VManageConnectionDrainingOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	nmaHealthOp := makeNMAHealthOp(options.Hosts)

	assertMainClusterUpNodes := options.Sandbox == ""

	// get up hosts in all sandboxes/clusters
	// exit early if specified sandbox has no up hosts
	// up hosts will be filtered by sandbox name in prepare stage of nmaManageConnectionsOp
	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesWithSandboxOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password,
		ManageConnectionDrainingCmd, options.Sandbox, assertMainClusterUpNodes)
	if err != nil {
		return instructions, err
	}

	nmaManageConnectionsOp, err := makeNMAManageConnectionsOp(options.Hosts,
		options.UserName, options.DBName, options.Sandbox, options.SCName,
		options.RedirectHostname, options.Action, options.Password,
		options.usePassword)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&httpsGetUpNodesOp,
		&nmaManageConnectionsOp,
	)

	return instructions, nil
}
