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

type VSandboxOptions struct {
	DatabaseOptions
	SandboxName string
	SCName      string
	SCHosts     []string
	// indicate whether a restore point is created when create the sandbox
	SaveRp bool
	// indicate whether the metadata of sandbox should be isolated
	Imeta bool
	// indicate whether the sandbox should create its own storage locations
	Sls bool
	// indicate wether the sandbox is being created for online upgrade
	ForUpgrade bool
	// The expected node names with their IPs in the subcluster, the user of vclusterOps needs
	// to make sure the provided values are correct. This option will be used to do re-ip in
	// the target sandbox.
	NodeNameAddressMap map[string]string
	// A primary up host in the target sandbox. This option will be used to do re-ip in
	// the target sandbox.
	SandboxPrimaryUpHost string
}

func VSandboxOptionsFactory() VSandboxOptions {
	options := VSandboxOptions{}
	options.setDefaultValues()
	return options
}

func (options *VSandboxOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
	options.NodeNameAddressMap = make(map[string]string)
}

func (options *VSandboxOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(SandboxSCCmd, logger)
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

	if options.SandboxName == "" {
		return fmt.Errorf("must specify a sandbox name")
	}

	err = util.ValidateSandboxName(options.SandboxName)
	if err != nil {
		return err
	}
	return nil
}

func (options *VSandboxOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}
	return nil
}

// resolve hostnames to be IPs
func (options *VSandboxOptions) analyzeOptions() (err error) {
	// we analyze hostnames when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VSandboxOptions) ValidateAnalyzeOptions(logger vlog.Printer) error {
	err := options.validateParseOptions(logger)
	if err != nil {
		return err
	}
	return options.analyzeOptions()
}

// produceSandboxSubclusterInstructions will build a list of instructions to execute for
// the sandbox subcluster operation.
//
// The generated instructions will later perform the following operations necessary
// for a successful sandbox_subcluster:
//   - Get UP nodes through HTTPS call, if any node is UP then the DB is UP and ready for running sandboxing operation
//   - Get subcluster sandbox information for the Up hosts. When we choose an initiator host for sandboxing,
//     This would help us filter out sandboxed Up hosts.
//     Also, we would want to filter out hosts from the subcluster to be sandboxed.
//   - Run Sandboxing for the user provided subcluster using the selected initiator host.
//   - Poll for the sandboxed subcluster hosts to be UP.

func (vcc *VClusterCommands) produceSandboxSubclusterInstructions(options *VSandboxOptions) ([]clusterOp, error) {
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

	username := options.UserName

	// Get all up nodes
	httpsGetUpNodesOp, err := makeHTTPSGetUpScNodesOp(options.DBName, options.Hosts,
		usePassword, username, options.Password, SandboxSCCmd, options.SCName)
	if err != nil {
		return instructions, err
	}

	// Get subcluster sandboxing information and remove sandboxed nodes from prospective initator hosts list
	httpsCheckSubclusterSandboxOp, err := makeHTTPSCheckSubclusterSandboxOp(options.Hosts,
		options.SCName, options.SandboxName, usePassword, username, options.Password)
	if err != nil {
		return instructions, err
	}

	// Run Sandboxing
	httpsSandboxSubclusterOp, err := makeHTTPSandboxingOp(vcc.Log, options.SCName, options.SandboxName,
		usePassword, username, options.Password, options.SaveRp, options.Imeta, options.Sls, options.ForUpgrade,
		&options.SCHosts)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsGetUpNodesOp,
		&httpsCheckSubclusterSandboxOp,
		&httpsSandboxSubclusterOp,
	)

	return instructions, nil
}

func (vcc VClusterCommands) VSandbox(options *VSandboxOptions) error {
	vcc.Log.V(0).Info("VSandbox method called", "options", options)
	return runSandboxCmd(vcc, options)
}

// sandboxInterface is an interface that will be used by runSandboxCmd().
// The purpose of this interface is to avoid code duplication.
type sandboxInterface interface {
	ValidateAnalyzeOptions(logger vlog.Printer) error
	runCommand(vcc VClusterCommands) error
}

// runCommand will produce instructions and run them
func (options *VSandboxOptions) runCommand(vcc VClusterCommands) error {
	// if the users want to do re-ip before sandboxing, we require them
	// to provide some node information
	if options.SandboxPrimaryUpHost != "" && len(options.NodeNameAddressMap) > 0 {
		err := vcc.reIP(&options.DatabaseOptions, options.SCName, options.SandboxPrimaryUpHost,
			options.NodeNameAddressMap, true /*reload spread*/)
		if err != nil {
			return err
		}
	}

	// make instructions
	instructions, err := vcc.produceSandboxSubclusterInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// add certs and instructions to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// run the engine
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to sandbox subcluster %s, %w", options.SCName, runError)
	}

	// assume the caller knows the status of the cluster better than us, override whatever the sandbox op set
	if len(options.NodeNameAddressMap) > 0 {
		options.SCHosts = []string{}
		for _, ip := range options.NodeNameAddressMap {
			options.SCHosts = append(options.SCHosts, ip)
		}
	}

	return nil
}

// runSandboxCmd is a help function to run sandbox/unsandbox command.
// It can avoid code duplication between VSandbox and VUnsandbox.
func runSandboxCmd(vcc VClusterCommands, i sandboxInterface) error {
	// check required options
	err := i.ValidateAnalyzeOptions(vcc.Log)
	if err != nil {
		vcc.Log.Error(err, "failed to validate the options")
		return err
	}

	return i.runCommand(vcc)
}
