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
	ControlSetSizeDefaultValue = -1
	ControlSetSizeLowerBound   = 1
	ControlSetSizeUpperBound   = 120
)

type VAddSubclusterOptions struct {
	// part 1: basic db info
	DatabaseOptions
	// part 2: subcluster info
	SCName         string
	IsPrimary      bool
	ControlSetSize int
	CloneSC        string
	// part 3: add node info
	VAddNodeOptions
}

type VAddSubclusterInfo struct {
	DBName         string
	Hosts          []string
	UserName       string
	Password       *string
	IsPrimary      bool
	ControlSetSize int
	CloneSC        string
}

func VAddSubclusterOptionsFactory() VAddSubclusterOptions {
	options := VAddSubclusterOptions{}
	// set default values to the params
	options.setDefaultValues()
	// set default values for VAddNodeOptions
	options.VAddNodeOptions.setDefaultValues()

	return options
}

func (options *VAddSubclusterOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()

	options.ControlSetSize = util.DefaultControlSetSize
}

func (options *VAddSubclusterOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(AddSubclusterCmd, logger)
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

	return nil
}

func (options *VAddSubclusterOptions) validateEonOptions() error {
	if !options.IsEon {
		return fmt.Errorf("add subcluster is only supported in Eon mode")
	}
	return nil
}

func (options *VAddSubclusterOptions) validateExtraOptions(logger vlog.Printer) error {
	// control-set-size can only be -1 or [1 to 120]
	if !(options.ControlSetSize == ControlSetSizeDefaultValue ||
		(options.ControlSetSize >= ControlSetSizeLowerBound && options.ControlSetSize <= ControlSetSizeUpperBound)) {
		return fmt.Errorf("control-set-size is out of bounds: valid values are %d or [%d to %d]",
			ControlSetSizeDefaultValue, ControlSetSizeLowerBound, ControlSetSizeUpperBound)
	}

	if options.CloneSC != "" {
		// TODO remove this log after we supported subcluster clone
		logger.PrintWarning("option CloneSC is not implemented yet so it will be ignored")
	}

	return nil
}

func (options *VAddSubclusterOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}
	// batch 2: validate eon params
	err = options.validateEonOptions()
	if err != nil {
		return err
	}
	// batch 3: validate all other params
	err = options.validateExtraOptions(logger)
	if err != nil {
		return err
	}
	return nil
}

// resolve hostnames to be IPs
func (options *VAddSubclusterOptions) analyzeOptions() (err error) {
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

func (options *VAddSubclusterOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	err := options.analyzeOptions()
	if err != nil {
		return err
	}
	return options.setUsePasswordAndValidateUsernameIfNeeded(logger)
}

// VAddSubcluster adds to a running database a new subcluster with provided options.
// It returns any error encountered.
func (vcc VClusterCommands) VAddSubcluster(options *VAddSubclusterOptions) error {
	/*
	 *   - Validate Options
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	instructions, err := vcc.produceAddSubclusterInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// Create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to add subcluster %s, %w", options.SCName, runError)
	}

	return nil
}

// produceAddSubclusterInstructions will build a list of instructions to execute for
// the add subcluster operation.
//
// The generated instructions will later perform the following operations necessary
// for a successful add_subcluster:
//   - Get cluster info from running db and exit error if the db is an enterprise db
//   - Get UP nodes through HTTPS call, if any node is UP then the DB is UP and ready for adding a new subcluster
//   - Add the subcluster catalog object through HTTPS call, and check the response to error out
//     if the subcluster name already exists
//   - Check if the new subcluster is created in database through HTTPS call
func (vcc *VClusterCommands) produceAddSubclusterInstructions(options *VAddSubclusterOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	vdb := makeVCoordinationDatabase()

	// NMA health check
	// this is not needed for adding subcluster
	// but if this failed, adding nodes may fail and may give users confusing messages
	nmaHealthOp := makeNMAHealthOp(options.Hosts)

	// get cluster info
	err := vcc.getClusterInfoFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return instructions, err
	}

	// add_subcluster only works with Eon database
	if !vdb.IsEon {
		// info from running db confirms that the db is not Eon
		return instructions, fmt.Errorf("add subcluster is only supported in Eon mode")
	}

	username := options.UserName
	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesOp(options.DBName, options.Hosts,
		options.usePassword, username, options.Password, AddSubclusterCmd)
	if err != nil {
		return instructions, err
	}

	httpsAddSubclusterOp, err := makeHTTPSAddSubclusterOp(options.usePassword, username, options.Password,
		options.SCName, options.IsPrimary, options.ControlSetSize)
	if err != nil {
		return instructions, err
	}

	httpsCheckSubclusterOp, err := makeHTTPSCheckSubclusterOp(options.usePassword, username, options.Password,
		options.SCName, options.IsPrimary, options.ControlSetSize)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&httpsGetUpNodesOp,
		&httpsAddSubclusterOp,
		&httpsCheckSubclusterOp,
	)

	return instructions, nil
}
