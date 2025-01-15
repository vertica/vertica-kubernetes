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

type VPromoteSandboxToMainOptions struct {
	// basic db info
	DatabaseOptions
	// Name of the sandbox to promote to main
	SandboxName string
}

func VPromoteSandboxToMainFactory() VPromoteSandboxToMainOptions {
	opt := VPromoteSandboxToMainOptions{}
	// set default values to the params
	opt.setDefaultValues()
	return opt
}

func (opt *VPromoteSandboxToMainOptions) validateEonOptions(_ vlog.Printer) error {
	if !opt.IsEon {
		return fmt.Errorf("promote a sandbox to main is only supported in Eon mode")
	}
	if opt.SandboxName == "" {
		return fmt.Errorf("must specify a sandbox name")
	}
	return nil
}

func (opt *VPromoteSandboxToMainOptions) validateParseOptions(logger vlog.Printer) error {
	err := opt.validateEonOptions(logger)
	if err != nil {
		return err
	}

	err = opt.validateBaseOptions(PromoteSandboxToMainCmd, logger)
	if err != nil {
		return err
	}

	return opt.validateAuthOptions("", logger)
}

// analyzeOptions will modify some options based on what is chosen
func (opt *VPromoteSandboxToMainOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(opt.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		hostAddresses, err := util.ResolveRawHostsToAddresses(opt.RawHosts, opt.IPv6)
		if err != nil {
			return err
		}
		opt.Hosts = hostAddresses
	}
	return nil
}

func (opt *VPromoteSandboxToMainOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := opt.validateParseOptions(logger); err != nil {
		return err
	}
	if err := opt.analyzeOptions(); err != nil {
		return err
	}
	if err := opt.setUsePassword(logger); err != nil {
		return err
	}
	// username is always required when local db connection is made
	return opt.validateUserName(logger)
}

// VPromoteSandboxToMain can convert local sandbox to main cluster. The conversion is supported only for
// special sandboxes: without meta-isolation and communal (prefix) isolation. Those can be created
// with the: "sls=false;imeta=false" options
func (vcc VClusterCommands) VPromoteSandboxToMain(options *VPromoteSandboxToMainOptions) error {
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

	// retrieve information from the database to accurately determine the state of each node in both the main cluster and sandbox
	vdb := makeVCoordinationDatabase()
	err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, options.SandboxName)
	if err != nil {
		return err
	}

	// produce sandbox to main cluster instructions
	instructions, err := vcc.promoteSandboxToMainInstructions(options, &vdb)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to promote a sandbox to main cluster: %w", runError)
	}

	return nil
}

// The generated instructions will later perform the following operations necessary
// for a successful promote sandbox operation:
// - pick one of the up nodes in the sandboxed subcluster as the initiator
// - check nma health on the initiator
// - promote sandbox to main on the initiator
// - clean communal storage on the initiator
func (vcc VClusterCommands) promoteSandboxToMainInstructions(options *VPromoteSandboxToMainOptions,
	vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp

	var upHost string
	for _, node := range vdb.HostNodeMap {
		if node.State == util.NodeDownState {
			continue
		}
		// the up host is used to promote the sandbox to main cluster
		// should be the one in the sandbox
		if node.Sandbox == options.SandboxName && node.Sandbox != "" {
			upHost = node.Address
			break
		}
	}
	initiator := []string{upHost}
	nmaHealthOp := makeNMAHealthOp(initiator)
	httpsConvertSandboxToMainOp, err := makeHTTPSConvertSandboxToMainOp(initiator,
		options.UserName, options.Password, options.usePassword, options.SandboxName)
	if err != nil {
		return nil, err
	}
	nmaCleanCommunalStorageOp, err := makeNMACleanCommunalStorageOp(initiator,
		options.UserName, options.DBName, options.Password, options.usePassword,
		false /* not only print invalid files in communal storage, but also delete them */)
	if err != nil {
		return nil, err
	}
	instructions = append(instructions, &nmaHealthOp, &httpsConvertSandboxToMainOp, &nmaCleanCommunalStorageOp)
	return instructions, nil
}
