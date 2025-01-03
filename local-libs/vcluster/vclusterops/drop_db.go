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
)

// VDropDatabaseOptions adds to VCreateDatabaseOptions the option to force delete directories.
type VDropDatabaseOptions struct {
	VCreateDatabaseOptions
	ForceDelete bool // whether force delete directories
}

func VDropDatabaseOptionsFactory() VDropDatabaseOptions {
	options := VDropDatabaseOptions{}
	// set default values to the params
	options.setDefaultValues()
	options.ForceDelete = true

	return options
}

// analyzeOptions verifies the host options for the VDropDatabaseOptions struct and
// returns any error encountered.
func (options *VDropDatabaseOptions) analyzeOptions() error {
	if len(options.RawHosts) > 0 {
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}

	return nil
}

func (options *VDropDatabaseOptions) validateParseOptions() error {
	if options.DBName == "" {
		return fmt.Errorf("database name must be provided")
	}

	err := util.ValidateDBName(options.DBName)
	if err != nil {
		return err
	}
	return nil
}

func (options *VDropDatabaseOptions) validateAnalyzeOptions() error {
	err := options.validateParseOptions()
	if err != nil {
		return err
	}

	return options.analyzeOptions()
}

func (vcc VClusterCommands) VDropDatabase(options *VDropDatabaseOptions) error {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// Analyze to produce vdb info for drop db use
	vdb := makeVCoordinationDatabase()

	err := options.validateAnalyzeOptions()
	if err != nil {
		return err
	}

	err = vdb.setFromBasicDBOptions(&options.VCreateDatabaseOptions)
	if err != nil {
		return err
	}

	// produce drop_db instructions
	instructions, err := vcc.produceDropDBInstructions(&vdb, options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to drop database: %w", runError)
	}

	return nil
}

// produceDropDBInstructions will build a list of instructions to execute for
// the drop db operation
//
// The generated instructions will later perform the following operations necessary
// for a successful drop_db:
//   - Check NMA connectivity
//   - Check to see if any dbs running
//   - Delete directories
func (vcc VClusterCommands) produceDropDBInstructions(vdb *VCoordinationDatabase, options *VDropDatabaseOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	hosts := vdb.HostList
	usePassword := false
	if options.Password != nil {
		usePassword = true
		err := options.validateUserName(vcc.Log)
		if err != nil {
			return instructions, err
		}
	}

	nmaHealthOp := makeNMAHealthOp(hosts)

	// when checking the running database,
	// drop_db has the same checking items with create_db
	checkDBRunningOp, err := makeHTTPSCheckRunningDBOp(hosts, usePassword,
		options.UserName, options.Password, DropDB)
	if err != nil {
		return instructions, err
	}

	nmaDeleteDirectoriesOp, err := makeNMADeleteDirectoriesOp(vdb, options.ForceDelete)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&checkDBRunningOp,
		&nmaDeleteDirectoriesOp,
	)

	return instructions, nil
}
