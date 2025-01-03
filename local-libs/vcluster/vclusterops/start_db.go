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

// VStartDatabaseOptions represents the available options when you start a database
// with VStartDatabase.
type VStartDatabaseOptions struct {
	// basic db info
	// Devloper Guide:
	// The --hosts flag for start_db is used to start sandboxed hosts
	// If you want to partial start a down db by using --hosts
	// You should input more than half of the primary nodes to meet the quorum requirement
	// If quorum requirement is not meet, the start_db process will hang until timeout
	// And you need to manually kill those startup failed vertica processes.
	DatabaseOptions
	// timeout for polling the states of all nodes in the database in HTTPSPollNodeStateOp
	StatePollingTimeout int
	// whether trim the input host list based on the catalog info
	TrimHostList bool
	Sandbox      string // Start db on given sandbox
	MainCluster  bool   // Start db on main cluster only
	// If the path is set, the NMA will store the Vertica start command at the path
	// instead of executing it. This is useful in containerized environments where
	// you may not want to have both the NMA and Vertica server in the same container.
	// This feature requires version 24.2.0+.
	StartUpConf string
	// whether the provided hosts are in a sandbox
	HostsInSandbox bool

	// whether the first time to start the database after revive
	FirstStartAfterRevive bool

	// whether input info is read from vcluster config file, used for quorum check
	ReadFromConfig bool
}

func VStartDatabaseOptionsFactory() VStartDatabaseOptions {
	options := VStartDatabaseOptions{}

	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VStartDatabaseOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
	// set default value to StatePollingTimeout
	options.StatePollingTimeout = util.DefaultStatePollingTimeout
}

func (options *VStartDatabaseOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(StartDBCmd, logger)
	if err != nil {
		return err
	}
	return options.validateCatalogPath()
}

func (options *VStartDatabaseOptions) validateEonOptions() error {
	if options.CommunalStorageLocation != "" {
		return util.ValidateCommunalStorageLocation(options.CommunalStorageLocation)
	}
	return nil
}

func (options *VStartDatabaseOptions) validateParseOptions(logger vlog.Printer) error {
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
	return nil
}

func (options *VStartDatabaseOptions) analyzeOptions() (err error) {
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

func (options *VStartDatabaseOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

func (vcc VClusterCommands) VStartDatabase(options *VStartDatabaseOptions) (vdbPtr *VCoordinationDatabase, err error) {
	/*
	 *   - Produce Instructions
	 *   - Create VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// validate and analyze all options
	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return nil, err
	}

	// VER-93369 may improve this if the CLI knows which nodes are primary
	// from the config file
	var vdb VCoordinationDatabase
	// retrieve database information from cluster_config.json for Eon databases
	if options.IsEon {
		const warningMsg = " for an Eon database, start_db after revive_db could fail " +
			util.DBInfo
		if options.CommunalStorageLocation != "" {
			vdbNew, e := options.getVDBFromSandboxWhenDBIsDown(vcc, options.Sandbox)
			if e != nil {
				// show a warning message if we cannot get VDB from a down database
				vcc.Log.PrintWarning(util.CommStorageFail + warningMsg)
			} else {
				// we want to read catalog info only from primary nodes later
				vdbNew.filterPrimaryNodes()
				vdb = vdbNew
			}
		} else {
			// When communal storage location is missing, we only log a warning message
			// because fail to read cluster_config.json will not affect start_db in most of the cases.
			vcc.Log.PrintWarning(util.CommStorageLoc + warningMsg)
		}
	}
	numTotalNodes := len(options.Hosts)

	// start_db pre-checks and get basic info
	unreachableHosts, err := vcc.runStartDBPrecheck(options, &vdb, numTotalNodes)
	if err != nil {
		return nil, err
	}

	// produce start_db instructions
	instructions, err := vcc.produceStartDBInstructions(options, &vdb, unreachableHosts)
	if err != nil {
		return nil, fmt.Errorf("fail to production instructions: %w", err)
	}

	// create a VClusterOpEngine for start_db instructions, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.runInSandbox(vcc.Log, &vdb, options.Sandbox)
	if runError != nil {
		return nil, fmt.Errorf("fail to start database: %w", runError)
	}

	// get vdb info from the running database
	var updatedVDB VCoordinationDatabase
	err = vcc.getVDBFromRunningDBIncludeSandbox(&updatedVDB, &options.DatabaseOptions, AnySandbox)
	if err != nil {
		return nil, err
	}

	return &updatedVDB, nil
}

func (vcc VClusterCommands) runStartDBPrecheck(options *VStartDatabaseOptions, vdb *VCoordinationDatabase,
	numTotalNodes int) ([]string, error) {
	// filter out unreachable hosts
	unreachableHosts, err := vcc.getUnreachableHosts(&options.DatabaseOptions, options.Hosts)
	if err != nil {
		return nil, err
	}
	// if it's eon mode and there are unreachable hosts, we cannot perform quorum check due to missing primary node information
	// error out here with hint
	if options.IsEon && len(unreachableHosts) > 0 {
		return nil, fmt.Errorf("cannot start db with unreachable hosts, please check cluster and NMA connectivity on unreachable hosts")
	}
	options.Hosts = util.SliceDiff(options.Hosts, unreachableHosts)
	// pre-instruction to perform basic checks and get basic information
	preInstructions, err := vcc.produceStartDBPreCheck(options, vdb, options.TrimHostList)
	if err != nil {
		return nil, fmt.Errorf("fail to production instructions: %w", err)
	}

	// create a VClusterOpEngine for pre-check, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(preInstructions, options)
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return nil, fmt.Errorf("fail to start database pre-checks: %w", runError)
	}

	// If requested, remove any provided hosts that are not in the catalog. Use
	// the vdb that we just fetched by the catalog editor. It will be the from
	// the latest catalog.
	if options.TrimHostList {
		options.Hosts = vcc.removeHostsNotInCatalog(&clusterOpEngine.execContext.nmaVDatabase, options.Hosts)
	}

	// Quorum Check
	if options.ReadFromConfig && !options.IsEon {
		err = vcc.quorumCheck(numTotalNodes, len(options.Hosts))
		if err != nil {
			return nil, fmt.Errorf("fail to start database pre-checks: %w", err)
		}
	}

	return unreachableHosts, nil
}

func (vcc VClusterCommands) removeHostsNotInCatalog(vdb *nmaVDatabase, hosts []string) []string {
	var trimmedHostList []string
	var extraHosts []string

	vcc.Log.Info("checking if any input hosts can be removed",
		"hosts", hosts, "hostNodeMap", vdb.HostNodeMap)
	for _, h := range hosts {
		if _, exist := vdb.HostNodeMap[h]; exist {
			trimmedHostList = append(trimmedHostList, h)
		} else {
			extraHosts = append(extraHosts, h)
		}
	}

	if len(extraHosts) > 0 {
		vcc.Log.PrintInfo("The following hosts will be trimmed as they are not found in catalog: %+v",
			extraHosts)
	}
	return trimmedHostList
}

// produceStartDBPreCheck will build a list of pre-check instructions to execute for
// the start_db command.
//
// The generated instructions will later perform the following operations necessary
// for a successful start_db:
//   - Check NMA connectivity
//   - Check to see if any dbs run
//   - Get nodes' information by calling the NMA /nodes endpoint
//   - Find latest catalog to use for removal of nodes not in the catalog
func (vcc VClusterCommands) produceStartDBPreCheck(options *VStartDatabaseOptions, vdb *VCoordinationDatabase,
	trimHostList bool) ([]clusterOp, error) {
	var instructions []clusterOp

	// need username for https operations
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}

	checkDBRunningOp, err := makeHTTPSCheckRunningDBOp(options.Hosts,
		options.usePassword, options.UserName, options.Password, StartDB)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &checkDBRunningOp)

	// when we cannot get db info from cluster_config.json, we will fetch it from NMA /nodes endpoint.
	if len(vdb.HostNodeMap) == 0 {
		nmaGetNodesInfoOp := makeNMAGetNodesInfoOp(options.Hosts, options.DBName, options.CatalogPrefix,
			true /* ignore internal errors */, vdb)
		instructions = append(instructions, &nmaGetNodesInfoOp)
	}

	// find latest catalog to use for removal of nodes not in the catalog
	if trimHostList {
		nmaReadCatalogEditorOp, err := makeNMAReadCatalogEditorOpForStartDB(vdb, options.FirstStartAfterRevive, options.Sandbox)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &nmaReadCatalogEditorOp)
	}

	return instructions, nil
}

// produceStartDBInstructions will build a list of instructions to execute for
// the start_db command.
//
// The generated instructions will later perform the following operations necessary
// for a successful start_db:
//   - Use NMA /catalog/database to get the best source node for spread.conf and vertica.conf
//   - Check Vertica versions
//   - Sync the confs to the rest of nodes who have lower catalog version (results from the previous step)
//   - Start all nodes of the database
//   - Poll node startup
//   - Sync catalog (Eon mode only)
func (vcc VClusterCommands) produceStartDBInstructions(options *VStartDatabaseOptions, vdb *VCoordinationDatabase,
	unreachableHosts []string) ([]clusterOp, error) {
	var instructions []clusterOp

	// vdb here should contain only primary nodes
	nmaReadCatalogEditorOp, err := makeNMAReadCatalogEditorOpForStartDB(vdb, options.FirstStartAfterRevive, options.Sandbox)
	if err != nil {
		return instructions, err
	}
	// require to have the same vertica version
	nmaVerticaVersionOp := makeNMAVerticaVersionOpWithTargetHosts(true, unreachableHosts, options.Hosts)
	instructions = append(instructions,
		&nmaReadCatalogEditorOp,
		&nmaVerticaVersionOp,
	)

	if enabled, keyType := options.isSpreadEncryptionEnabled(); enabled {
		instructions = append(instructions,
			vcc.setOrRotateEncryptionKey(keyType),
		)
	}

	// sourceConfHost is set to nil value in upload and download step
	// we use information from catalog editor operation to update the sourceConfHost value
	// after we find host with the highest catalog and hosts that need to synchronize the catalog
	// we will remove the nil parameters in VER-88401 by adding them in execContext
	produceTransferConfigOps(
		&instructions,
		nil, /*source hosts for transferring configuration files*/
		options.Hosts,
		nil, /*db configurations retrieved from a running db*/
		nil /*sandbox name*/)

	nmaStartNewNodesOp := makeNMAStartNodeOp(options.Hosts, options.StartUpConf)
	httpsPollNodeStateOp, err := makeHTTPSPollNodeStateOp(options.Hosts,
		options.usePassword, options.UserName, options.Password, options.StatePollingTimeout)
	if err != nil {
		return instructions, err
	}
	httpsPollNodeStateOp.cmdType = StartDBCmd

	instructions = append(instructions,
		&nmaStartNewNodesOp,
		&httpsPollNodeStateOp,
	)

	if options.IsEon {
		httpsSyncCatalogOp, err := makeHTTPSSyncCatalogOp(options.Hosts, options.usePassword, options.UserName, options.Password, StartDBSyncCat)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsSyncCatalogOp)
	}

	return instructions, nil
}

func (vcc VClusterCommands) setOrRotateEncryptionKey(keyType string) clusterOp {
	vcc.Log.Info("adding instruction to set or rotate the key for spread encryption")
	op := makeNMASpreadSecurityOp(vcc.Log, keyType)
	return &op
}

func (vcc VClusterCommands) quorumCheck(numPrimaryNodes, numReachableHosts int) error {
	minimumNodesForQuorum := numPrimaryNodes/2 + 1
	if numReachableHosts < minimumNodesForQuorum {
		return fmt.Errorf("quorum not satisfied, number of reachable nodes %d < minimum %d of %d primary nodes",
			numReachableHosts, minimumNodesForQuorum, numPrimaryNodes)
	}
	return nil
}
