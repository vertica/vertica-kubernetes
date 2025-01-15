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

type VFetchCoordinationDatabaseOptions struct {
	DatabaseOptions
	Overwrite   bool // overwrite existing config file at the same location
	AfterRevive bool // whether recover config right after revive_db

	// hidden option
	readOnly bool // this should be only used if we don't want to update the config file
}

func VRecoverConfigOptionsFactory() VFetchCoordinationDatabaseOptions {
	options := VFetchCoordinationDatabaseOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VFetchCoordinationDatabaseOptions) validateParseOptions(logger vlog.Printer) error {
	return options.validateBaseOptions(ConfigRecoverCmd, logger)
}

func (options *VFetchCoordinationDatabaseOptions) analyzeOptions() error {
	// resolve RawHosts to be IP addresses
	if len(options.RawHosts) > 0 {
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}

	// process correct catalog path
	options.CatalogPrefix = util.GetCleanPath(options.CatalogPrefix)

	// check existing config file at the same location
	if !options.readOnly && !options.Overwrite {
		if util.CanWriteAccessPath(options.ConfigPath) == util.FileExist {
			return fmt.Errorf("config file exists at %s. "+
				"You can use --overwrite to overwrite this existing config file", options.ConfigPath)
		}
	}

	return nil
}

func (options *VFetchCoordinationDatabaseOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

func (vcc VClusterCommands) VFetchCoordinationDatabase(options *VFetchCoordinationDatabaseOptions) (VCoordinationDatabase, error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	var vdb VCoordinationDatabase

	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return vdb, err
	}

	// pre-fill vdb from the user input
	vdb.Name = options.DBName
	vdb.HostList = options.Hosts
	vdb.CatalogPrefix = options.CatalogPrefix
	vdb.DepotPrefix = options.DepotPrefix
	vdb.Ipv6 = options.IPv6

	// produce list_all_nodes instructions
	instructions, err := vcc.produceRecoverConfigInstructions(options, &vdb)
	if err != nil {
		return vdb, fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)

	// nmaVDB is an object obtained from the read catalog editor result
	// we use nmaVDB data to complete vdb
	nmaVDB := clusterOpEngine.execContext.nmaVDatabase

	if !options.readOnly && nmaVDB.CommunalStorageLocation != "" {
		vdb.IsEon = true
		vdb.CommunalStorageLocation = nmaVDB.CommunalStorageLocation
		// if depot path is not provided for an Eon DB,
		// we should error out
		if vdb.DepotPrefix == "" {
			return vdb,
				fmt.Errorf("the depot path must be provided for an Eon database")
		}
	}

	for h, n := range nmaVDB.HostNodeMap {
		if h == util.UnboundedIPv4 || h == util.UnboundedIPv6 {
			continue
		}
		vnode, ok := vdb.HostNodeMap[h]
		if !ok {
			return vdb, fmt.Errorf("host %s is not found in the vdb object", h)
		}
		vnode.Subcluster = n.Subcluster.Name
		vnode.StorageLocations = n.StorageLocations
		vnode.IsPrimary = n.IsPrimary
		vnode.Sandbox = n.Subcluster.SandboxName
	}

	return vdb, runError
}

// produceRecoverConfigInstructions will build a list of instructions to execute for
// the recover config operation.

// The generated instructions will later perform the following operations necessary
// for a successful `manage_config --recover`
//   - Check NMA connectivity
//   - Get information of nodes
//   - Read catalog editor
func (vcc VClusterCommands) produceRecoverConfigInstructions(
	options *VFetchCoordinationDatabaseOptions,
	vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp

	nmaHealthOp := makeNMAHealthOp(options.Hosts)
	instructions = append(instructions, &nmaHealthOp)

	// Try fetching nodes info from a running db, if possible.
	err := vcc.getVDBFromRunningDBIncludeSandbox(vdb, &options.DatabaseOptions, AnySandbox)
	if err != nil {
		vcc.PrintWarning("No running db found. For eon db, restart the database to recover accurate sandbox information")
		nmaGetNodesInfoOp := makeNMAGetNodesInfoOp(options.Hosts, options.DBName, options.CatalogPrefix,
			true /* ignore internal errors */, vdb)
		nmaReadCatalogEditorOp, err := makeNMAReadCatalogEditorOp(vdb)
		if err != nil {
			return instructions, err
		}
		instructions = append(
			instructions,
			&nmaGetNodesInfoOp,
			&nmaReadCatalogEditorOp)
	}
	nmaReadVerticaVersionOp := makeNMAReadVerticaVersionOp(vdb)

	instructions = append(
		instructions,
		&nmaReadVerticaVersionOp,
	)

	return instructions, nil
}
