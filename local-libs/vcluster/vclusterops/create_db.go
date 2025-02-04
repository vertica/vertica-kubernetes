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
	"regexp"
	"strconv"
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// VCreateDatabaseOptions represents the available options when you create a database with
// VCreateDatabase.
type VCreateDatabaseOptions struct {
	/* part 1: basic db info */

	DatabaseOptions
	Policy            string // database restart policy
	SQLFile           string // SQL file to run (as dbadmin) immediately on database creation
	LicensePathOnNode string // required to be a fully qualified path
	EnableTLSAuth     bool   // enable TLS authentication immediately on database creation

	/* part 2: eon db info */

	ShardCount               int    // number of shards in the database"
	DepotSize                string // depot size with two supported formats: % and KMGT, e.g., 50% or 10G
	GetAwsCredentialsFromEnv bool   // whether get AWS credentials from environmental variables
	// part 3: optional info
	ForceCleanupOnFailure     bool // whether force remove existing directories on failure
	ForceRemovalAtCreation    bool // whether force remove existing directories before creating the database
	ForceOverwriteFile        bool // whether force overwrite existing config and config param files
	SkipPackageInstall        bool // whether skip package installation
	TimeoutNodeStartupSeconds int  // timeout in seconds for polling node start up state

	/* part 3: new params originally in installer generated admintools.conf, now in create db op */

	Broadcast          bool // configure Spread to use UDP broadcast traffic between nodes on the same subnet
	P2p                bool // configure Spread to use point-to-point communication between all Vertica nodes
	LargeCluster       int  // whether enables a large cluster layout
	ClientPort         int  // for internal QA test only, do not abuse
	SpreadLogging      bool // whether enable spread logging
	SpreadLoggingLevel int  // spread logging level

	/* part 4: other params */

	SkipStartupPolling bool // whether skip startup polling
	GenerateHTTPCerts  bool // whether generate http certificates
	// If the path is set, the NMA will store the Vertica start command at the path
	// instead of executing it. This is useful in containerized environments where
	// you may not want to have both the NMA and Vertica server in the same container.
	// This feature requires version 24.2.0+.
	StartUpConf string

	/* hidden options (which cache information only) */

	// the host used for bootstrapping
	bootstrapHost []string
}

func VCreateDatabaseOptionsFactory() VCreateDatabaseOptions {
	options := VCreateDatabaseOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VCreateDatabaseOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()

	// basic db info
	defaultPolicy := util.DefaultRestartPolicy
	options.Policy = defaultPolicy

	// optional info
	options.TimeoutNodeStartupSeconds = util.DefaultTimeoutSeconds

	// new params originally in installer generated admintools.conf, now in create db op
	options.P2p = util.DefaultP2p
	options.LargeCluster = util.DefaultLargeCluster
	options.ClientPort = util.DefaultClientPort
	options.SpreadLoggingLevel = util.DefaultSpreadLoggingLevel
	// specify whether to enable TLS authentication method upon database creation
	options.EnableTLSAuth = false
}

func (options *VCreateDatabaseOptions) validateRequiredOptions(logger vlog.Printer) error {
	// validate base options
	err := options.validateBaseOptions(CreateDBCmd, logger)
	if err != nil {
		return err
	}

	// validate required parameters with default values
	if options.Password == nil {
		options.Password = new(string)
		logger.Info("no password specified, using none")
	}

	if !util.StringInArray(options.Policy, util.RestartPolicyList) {
		return fmt.Errorf("policy must be one of %v", util.RestartPolicyList)
	}

	// MUST provide a fully qualified path,
	// because vcluster could be executed outside of Vertica cluster hosts
	// so no point to resolve relative paths to absolute paths by checking
	// localhost, where vcluster is run
	//
	// empty string ("") will be converted to the default license path (/opt/vertica/share/license.key)
	// in the /bootstrap-catalog endpoint
	if options.LicensePathOnNode != "" && !util.IsAbsPath(options.LicensePathOnNode) {
		return fmt.Errorf("must provide a fully qualified path for license file")
	}

	return nil
}

func validateDepotSizePercent(size string) (bool, error) {
	if !strings.Contains(size, "%") {
		return true, nil
	}
	cleanSize := strings.TrimSpace(size)
	// example percent depot size: '40%'
	r := regexp.MustCompile(`^([-+]?\d+)(%)$`)

	// example of matches: [[40%, 40, %]]
	matches := r.FindAllStringSubmatch(cleanSize, -1)

	if len(matches) != 1 {
		return false, fmt.Errorf("%s is not a well-formatted whole-number percentage of the format <int>%%", size)
	}

	valueStr := matches[0][1]
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return false, fmt.Errorf("%s is not a well-formatted whole-number percent of the format <int>%%", size)
	}

	if value > util.MaxDepotSize {
		return false, fmt.Errorf("depot-size %s is invalid, because it is greater than 100%%", size)
	} else if value < util.MinDepotSize {
		return false, fmt.Errorf("depot-size %s is invalid, because it is less than 0%%", size)
	}

	return true, nil
}

func validateDepotSizeBytes(size string) (bool, error) {
	// no need to validate for bytes if string contains '%'
	if strings.Contains(size, "%") {
		return true, nil
	}
	cleanSize := strings.TrimSpace(size)

	// example depot size: 1024K, 1024M, 2048G, 400T
	r := regexp.MustCompile(`^([-+]?\d+)([KMGT])$`)
	matches := r.FindAllStringSubmatch(cleanSize, -1)
	if len(matches) != 1 {
		return false, fmt.Errorf("%s is not a well-formatted whole-number size in bytes of the format <int>[KMGT]", size)
	}

	valueStr := matches[0][1]
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return false, fmt.Errorf("depot size %s is not a well-formatted whole-number size in bytes of the format <int>[KMGT]", size)
	}
	if value <= 0 {
		return false, fmt.Errorf("depot size %s is not a valid size because it is <= 0", size)
	}
	return true, nil
}

// may need to go back to consolt print for vcluster commands
// so return error information
func validateDepotSize(size string) (bool, error) {
	validDepotPercent, err := validateDepotSizePercent(size)
	if !validDepotPercent {
		return validDepotPercent, err
	}
	validDepotBytes, err := validateDepotSizeBytes(size)
	if !validDepotBytes {
		return validDepotBytes, err
	}
	return true, nil
}

func (options *VCreateDatabaseOptions) validateEonOptions() error {
	if options.CommunalStorageLocation != "" {
		err := util.ValidateCommunalStorageLocation(options.CommunalStorageLocation)
		if err != nil {
			return err
		}
		if options.DepotPrefix == "" {
			return fmt.Errorf("must specify a depot path with commual storage location")
		}
		if options.ShardCount == 0 {
			return fmt.Errorf("must specify a shard count greater than 0 with communal storage location")
		}
	}
	if options.DepotPrefix != "" && options.CommunalStorageLocation == "" {
		return fmt.Errorf("when depot path is given, communal storage location cannot be empty")
	}
	if options.GetAwsCredentialsFromEnv && options.CommunalStorageLocation == "" {
		return fmt.Errorf("AWS credentials are only used in Eon mode")
	}
	if options.DepotSize != "" {
		if options.DepotPrefix == "" {
			return fmt.Errorf("when depot size is given, depot path cannot be empty")
		}
		validDepotSize, err := validateDepotSize(options.DepotSize)
		if !validDepotSize {
			return err
		}
	}
	return nil
}

func (options *VCreateDatabaseOptions) validateExtraOptions() error {
	if options.Broadcast && options.P2p {
		return fmt.Errorf("cannot use both Broadcast and Point-to-point networking mode")
	}
	// -1 is the default large cluster value, meaning 120 control nodes
	if options.LargeCluster != util.DefaultLargeCluster && (options.LargeCluster < 1 || options.LargeCluster > util.MaxLargeCluster) {
		return fmt.Errorf("must specify a valid large cluster value in range [1, 120]")
	}
	return nil
}

func (options *VCreateDatabaseOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters without default values
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
	err = options.validateExtraOptions()
	if err != nil {
		return err
	}
	return nil
}

// Do advanced analysis on the options inputs, like resolve hostnames to be IPs
func (options *VCreateDatabaseOptions) analyzeOptions() error {
	// resolve RawHosts to be IP addresses
	if len(options.RawHosts) > 0 {
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}

	// process correct catalog path, data path and depot path prefixes
	options.CatalogPrefix = util.GetCleanPath(options.CatalogPrefix)
	options.DataPrefix = util.GetCleanPath(options.DataPrefix)
	options.DepotPrefix = util.GetCleanPath(options.DepotPrefix)

	return nil
}

func (options *VCreateDatabaseOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

func (vcc VClusterCommands) VCreateDatabase(options *VCreateDatabaseOptions) (VCoordinationDatabase, error) {
	vcc.Log.Info("starting VCreateDatabase")

	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */
	// Analyze to produce vdb info, for later create db use and for cache db info
	vdb := makeVCoordinationDatabase()
	err := vdb.setFromCreateDBOptions(options, vcc.Log)
	if err != nil {
		vcc.Log.Error(err, "fail to create database")
		return vdb, err
	}
	// produce instructions
	instructions, err := vcc.produceCreateDBInstructions(&vdb, options)
	if err != nil {
		vcc.Log.Error(err, "fail to produce create db instructions")
		return vdb, err
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	err = clusterOpEngine.run(vcc.Log)
	if err != nil {
		vcc.Log.Error(err, "fail to create database")
		return vdb, err
	}
	return vdb, nil
}

// produceCreateDBInstructions will build a list of instructions to execute for
// the create db operation.
//
// The generated instructions will later perform the following operations necessary
// for a successful create_db:
//   - Check NMA connectivity
//   - Check to see if any dbs running
//   - Check NMA versions
//   - Prepare directories
//   - Get network profiles
//   - Bootstrap the database
//   - Run the catalog editor
//   - Start bootstrap node
//   - Wait for the bootstrapped node to be UP
//   - Create other nodes
//   - Reload spread
//   - Transfer config files
//   - Start all nodes of the database
//   - Poll node startup
//   - Create depot (Eon mode only)
//   - Mark design ksafe
//   - Install packages
//   - Enable TLS authentication if needed
//   - Sync catalog
func (vcc VClusterCommands) produceCreateDBInstructions(
	vdb *VCoordinationDatabase,
	options *VCreateDatabaseOptions) ([]clusterOp, error) {
	instructions, err := vcc.produceCreateDBBootstrapInstructions(vdb, options)
	if err != nil {
		return instructions, err
	}

	workerNodesInstructions, err := vcc.produceCreateDBWorkerNodesInstructions(vdb, options)
	if err != nil {
		return instructions, err
	}

	additionalInstructions, err := vcc.produceAdditionalCreateDBInstructions(vdb, options)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions, workerNodesInstructions...)
	instructions = append(instructions, additionalInstructions...)

	return instructions, nil
}

// produceCreateDBBootstrapInstructions returns the bootstrap instructions for create_db.
func (vcc VClusterCommands) produceCreateDBBootstrapInstructions(
	vdb *VCoordinationDatabase,
	options *VCreateDatabaseOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	hosts := vdb.HostList
	initiator := getInitiator(hosts)

	nmaHealthOp := makeNMAHealthOp(hosts)

	// require to have the same vertica version
	nmaVerticaVersionOp := makeNMACheckVerticaVersionOp(hosts, true, vdb.IsEon)

	// need username for https operations
	err := options.validateUserName(vcc.Log)
	if err != nil {
		return instructions, err
	}

	checkDBRunningOp, err := makeHTTPSCheckRunningDBOp(hosts, true, /* use password auth */
		options.UserName, options.Password, CreateDB)
	if err != nil {
		return instructions, err
	}

	nmaPrepareDirectoriesOp, err := makeNMAPrepareDirectoriesOp(vdb.HostNodeMap,
		options.ForceRemovalAtCreation, false /*for db revive*/)
	if err != nil {
		return instructions, err
	}

	nmaNetworkProfileOp := makeNMANetworkProfileOp(hosts)

	// should be only one bootstrap host
	// making it an array to follow the convention of passing a list of hosts to each operation
	bootstrapHost := []string{initiator}
	options.bootstrapHost = bootstrapHost
	nmaBootstrapCatalogOp, err := makeNMABootstrapCatalogOp(vdb, options, bootstrapHost)
	if err != nil {
		return instructions, err
	}

	nmaReadCatalogEditorOp, err := makeNMAReadCatalogEditorOpWithInitiator(bootstrapHost, vdb)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaVerticaVersionOp,
		&checkDBRunningOp,
		&nmaPrepareDirectoriesOp,
		&nmaNetworkProfileOp,
		&nmaBootstrapCatalogOp,
		&nmaReadCatalogEditorOp,
	)

	if enabled, keyType := options.isSpreadEncryptionEnabled(); enabled {
		instructions = append(instructions,
			vcc.addEnableSpreadEncryptionOp(keyType),
		)
	}

	nmaStartNodeOp := makeNMAStartNodeOp(bootstrapHost, options.StartUpConf)

	httpsPollBootstrapNodeStateOp, err := makeHTTPSPollNodeStateOp(bootstrapHost, true, /* useHTTPPassword */
		options.UserName, options.Password, options.TimeoutNodeStartupSeconds)
	if err != nil {
		return instructions, err
	}
	httpsPollBootstrapNodeStateOp.cmdType = CreateDBCmd

	instructions = append(instructions,
		&nmaStartNodeOp,
		&httpsPollBootstrapNodeStateOp,
	)

	return instructions, nil
}

// produceCreateDBWorkerNodesInstructions returns the workder nodes' instructions for create_db.
func (vcc VClusterCommands) produceCreateDBWorkerNodesInstructions(
	vdb *VCoordinationDatabase,
	options *VCreateDatabaseOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	hosts := vdb.HostList
	bootstrapHost := options.bootstrapHost

	newNodeHosts := util.SliceDiff(hosts, bootstrapHost)
	if len(hosts) > 1 {
		httpsCreateNodeOp, err := makeHTTPSCreateNodeOp(newNodeHosts, bootstrapHost,
			true /* use password auth */, options.UserName, options.Password, vdb, "" /* subcluster */, "" /* compute group */)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsCreateNodeOp)
	}

	httpsReloadSpreadOp, err := makeHTTPSReloadSpreadOpWithInitiator(bootstrapHost,
		true /* use password auth */, options.UserName, options.Password)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &httpsReloadSpreadOp)

	if len(hosts) > 1 {
		httpsGetNodesInfoOp, err := makeHTTPSGetNodesInfoOp(options.DBName, bootstrapHost,
			true /* use password auth */, options.UserName, options.Password, vdb, false, util.MainClusterSandbox)
		if err != nil {
			return instructions, err
		}

		httpsStartUpCommandOp, err := makeHTTPSStartUpCommandOp(true, /*use https password*/
			options.UserName, options.Password, vdb)
		if err != nil {
			return instructions, err
		}

		instructions = append(instructions, &httpsGetNodesInfoOp, &httpsStartUpCommandOp)

		produceTransferConfigOps(
			&instructions,
			bootstrapHost,
			vdb.HostList,
			vdb, /*db configurations retrieved from a running db*/
			nil /*sandbox name*/)
		nmaStartNewNodesOp := makeNMAStartNodeOpWithVDB(newNodeHosts, options.StartUpConf, vdb)
		instructions = append(instructions, &nmaStartNewNodesOp)
	}

	return instructions, nil
}

// produceAdditionalCreateDBInstructions returns additional instruction necessary for create_db.
func (vcc VClusterCommands) produceAdditionalCreateDBInstructions(vdb *VCoordinationDatabase,
	options *VCreateDatabaseOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	hosts := vdb.HostList
	bootstrapHost := options.bootstrapHost
	username := options.UserName

	if !options.SkipStartupPolling {
		httpsPollNodeStateOp, err := makeHTTPSPollNodeStateOp(hosts, true, username, options.Password,
			options.TimeoutNodeStartupSeconds)
		if err != nil {
			return instructions, err
		}
		httpsPollNodeStateOp.cmdType = CreateDBCmd
		instructions = append(instructions, &httpsPollNodeStateOp)
	}

	if vdb.UseDepot {
		httpsCreateDepotOp, err := makeHTTPSCreateClusterDepotOp(vdb, bootstrapHost, true, username, options.Password)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsCreateDepotOp)
	}

	if len(hosts) >= ksafetyThreshold {
		httpsMarkDesignKSafeOp, err := makeHTTPSMarkDesignKSafeOp(bootstrapHost, true, username,
			options.Password, ksafeValueOne)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsMarkDesignKSafeOp)
	}

	if !options.SkipPackageInstall {
		httpsInstallPackagesOp, err := makeHTTPSInstallPackagesOp(bootstrapHost, true, username, options.Password,
			false /* forceReinstall */, true /* verbose */)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsInstallPackagesOp)
	}
	if options.EnableTLSAuth {
		authName := util.DefaultIPv4AuthName
		authHosts := util.DefaultIPv4AuthHosts
		if options.IPv6 {
			authName = util.DefaultIPv6AuthName
			authHosts = util.DefaultIPv6AuthHosts
		}
		httpsCreateTLSAuthOp, err := makeHTTPSCreateTLSAuthOp(bootstrapHost, true /* use password */, username, options.Password,
			authName, authHosts)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsCreateTLSAuthOp)

		httpsGrantTLSAuthOp, err := makeHTTPSGrantTLSAuthOp(bootstrapHost, true /* use password */, username, options.Password,
			authName, username /*grantee of tls auth*/)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsGrantTLSAuthOp)
	}
	if vdb.IsEon {
		httpsSyncCatalogOp, err := makeHTTPSSyncCatalogOp(bootstrapHost, true, username, options.Password, CreateDBSyncCat)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &httpsSyncCatalogOp)
	}
	return instructions, nil
}

func (vcc VClusterCommands) addEnableSpreadEncryptionOp(keyType string) clusterOp {
	vcc.Log.Info("adding instruction to set key for spread encryption")
	op := makeNMASpreadSecurityOp(vcc.Log, keyType)
	return &op
}
