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
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type ReplicationOptions struct {
	TableOrSchemaName string
	IncludePattern    string
	ExcludePattern    string
	TargetNamespace   string
}

type VReplicationDatabaseOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	/* part 2: replication info */
	TargetDB        DatabaseOptions
	SourceTLSConfig string
	SandboxName     string
	Async           bool
	ReplicationOptions
}

func VReplicationDatabaseFactory() VReplicationDatabaseOptions {
	options := VReplicationDatabaseOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VReplicationDatabaseOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(ReplicationStartCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VReplicationDatabaseOptions) validateEonOptions() error {
	if !options.IsEon {
		return fmt.Errorf("replication is only supported in Eon mode")
	}
	return nil
}

func (options *VReplicationDatabaseOptions) validateExtraOptions() error {
	if len(options.TargetDB.Hosts) == 0 {
		return fmt.Errorf("must specify a target host or target host list")
	}

	// valiadate target database
	if options.TargetDB.DBName == "" {
		return fmt.Errorf("must specify a target database name")
	}
	err := util.ValidateDBName(options.TargetDB.DBName)
	if err != nil {
		return err
	}

	// need to provide a password or TLSconfig if source and target username are different
	if options.TargetDB.UserName != options.UserName {
		if options.TargetDB.Password == nil && options.SourceTLSConfig == "" {
			return fmt.Errorf("only trust authentication can support username without password or TLSConfig")
		}
	}

	if options.SandboxName != "" {
		err := util.ValidateSandboxName(options.SandboxName)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VReplicationDatabaseOptions) validateFineGrainedReplicationOptions() error {
	if options.TableOrSchemaName != "" {
		err := util.ValidateQualifiedObjectNamePattern(options.TableOrSchemaName, false)
		if err != nil && strings.HasPrefix(err.Error(), "invalid pattern") {
			return fmt.Errorf("pattern %s not allowed in --table-or-schema-name. HINT: use --include-pattern", options.TableOrSchemaName)
		}
	}

	if options.IncludePattern != "" {
		err := util.ValidateQualifiedObjectNamePattern(options.IncludePattern, true)
		if err != nil {
			return err
		}
	}

	if options.ExcludePattern != "" {
		err := util.ValidateQualifiedObjectNamePattern(options.ExcludePattern, true)
		if err != nil {
			return err
		}
	}

	if options.TargetNamespace != "" {
		err := util.ValidateName(options.TargetNamespace, "target-namespace", true)
		if err != nil {
			return err
		}
	}
	return nil
}

func (options *VReplicationDatabaseOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required params
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	// batch 2: validate eon params
	err = options.validateEonOptions()
	if err != nil {
		return err
	}

	// batch 3: validate auth params
	err = options.validateAuthOptions(ReplicationStartCmd.CmdString(), logger)
	if err != nil {
		return err
	}

	// batch 4: validate all other params
	err = options.validateExtraOptions()
	if err != nil {
		return err
	}

	// batch 5: validate fine-grained database replication options
	err = options.validateFineGrainedReplicationOptions()
	if err != nil {
		return err
	}
	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VReplicationDatabaseOptions) analyzeOptions() (err error) {
	if len(options.TargetDB.Hosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.TargetDB.Hosts, err = util.ResolveRawHostsToAddresses(options.TargetDB.Hosts, options.TargetDB.IPv6)
		if err != nil {
			return err
		}
	}
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
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

func (options *VReplicationDatabaseOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VReplicateDatabase can copy all table data and metadata from this cluster to another
func (vcc VClusterCommands) VReplicateDatabase(options *VReplicationDatabaseOptions) (int64, error) {
	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return 0, err
	}

	// retrieve information from the database to accurately determine the state of each node in both the main cluster and a given sandbox
	vdb := makeVCoordinationDatabase()
	err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, options.SandboxName)
	if err != nil {
		return 0, err
	}

	asyncReplicationTransactionID := new(int64)
	if options.Async {
		err := vcc.replicateDatabaseAsync(options, &vdb, asyncReplicationTransactionID)
		if err != nil {
			return 0, err
		}
	} else {
		err := vcc.replicateDatabaseSync(options, &vdb)
		if err != nil {
			return 0, err
		}
	}
	return *asyncReplicationTransactionID, nil
}

// Perform asynchronous database replication
func (vcc VClusterCommands) replicateDatabaseAsync(options *VReplicationDatabaseOptions,
	vdb *VCoordinationDatabase, asyncReplicationTransactionID *int64) error {
	/*
	 * Async replication steps:
	 * - (on target) Run NMA health check, get a list of existing transaction IDs
	 * - (on source) Run NMA health check, start asynchronous replication
	 * - (on target) Poll NMA for a new transaction ID - this is the ID for the new asynchronous replication operation
	 *
	 * Since source and target NMA certs can be different (VER-96992), we have to create multiple VClusterOpEngines to
	 * perform these steps. For each step:
	 * - Produce Instructions
	 * - Create a VClusterOpEngine with the correct certs (source or target)
	 * - Give the instructions to the VClusterOpEngine to run
	 */

	// need username for https operations in source database
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return err
	}

	// verify the username for connecting to the target database
	targetUsePassword := false
	if options.TargetDB.Password != nil {
		targetUsePassword = true
		if options.TargetDB.UserName == "" {
			username, e := util.GetCurrentUsername()
			if e != nil {
				return e
			}
			options.TargetDB.UserName = username
		}
		vcc.Log.Info("Current target username", "username", options.TargetDB.UserName)
	}

	// Produce instructions for target NMA health check and getting a list of existing transaction IDs
	transactionIDs := &[]int64{}
	instructions, err := vcc.produceGetTransactionIDsInstructions(options, targetUsePassword, transactionIDs)
	if err != nil {
		return fmt.Errorf("fail to produce instructions for getting existing transaction IDs, %w", err)
	}

	// Create a VClusterOpEngine, and add target certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, &options.TargetDB)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to get existing transaction IDs: %w", runError)
	}

	// Produce instructions for starting async replication
	instructions, err = vcc.produceStartAsyncReplicationInstructions(options, vdb, targetUsePassword)
	if err != nil {
		return fmt.Errorf("fail to produce instructions for starting replication, %w", err)
	}

	// create a VClusterOpEngine, and add source certs to the engine
	clusterOpEngine = makeClusterOpEngine(instructions, &options.DatabaseOptions)

	// give the instructions to the VClusterOpEngine to run
	runError = clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to start replication: %w", runError)
	}

	// Produce instructions for getting a new transaction ID to identify the async replication operation
	instructions, err = vcc.produceGetNewTransactionIDInstructions(options, vdb, targetUsePassword,
		transactionIDs, asyncReplicationTransactionID)
	if err != nil {
		return fmt.Errorf("fail to produce instructions for getting transaction ID, %w", err)
	}

	// Create a VClusterOpEngine, and add target certs to the engine
	clusterOpEngine = makeClusterOpEngine(instructions, &options.TargetDB)

	// Give the instructions to the VClusterOpEngine to run
	runError = clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to get transaction ID: %w", runError)
	}

	return nil
}

func (vcc VClusterCommands) produceGetTransactionIDsInstructions(options *VReplicationDatabaseOptions,
	targetUsePassword bool, transactionIDs *[]int64) ([]clusterOp, error) {
	var instructions []clusterOp

	nmaHealthOp := makeNMAHealthOp(options.TargetDB.Hosts)

	// Retrieve a list of transaction IDs before async replication starts
	nmaReplicationStatusData := nmaReplicationStatusRequestData{}
	nmaReplicationStatusData.DBName = options.TargetDB.DBName
	nmaReplicationStatusData.ExcludedTransactionIDs = []int64{} // Get all transaction IDs
	nmaReplicationStatusData.GetTransactionIDsOnly = true       // We only care about transaction IDs here
	nmaReplicationStatusData.TransactionID = 0                  // Set this to 0 so NMA returns all IDs
	nmaReplicationStatusData.UserName = options.TargetDB.UserName
	nmaReplicationStatusData.Password = options.TargetDB.Password

	nmaReplicationStatusOp, err := makeNMAReplicationStatusOp(options.TargetDB.Hosts, targetUsePassword,
		&nmaReplicationStatusData, transactionIDs, nil)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaReplicationStatusOp,
	)

	return instructions, nil
}

func (vcc VClusterCommands) produceStartAsyncReplicationInstructions(options *VReplicationDatabaseOptions,
	vdb *VCoordinationDatabase, targetUsePassword bool) ([]clusterOp, error) {
	var instructions []clusterOp

	initiatorTargetHost := getInitiator(options.TargetDB.Hosts)

	nmaHealthOp := makeNMAHealthOp(options.Hosts)

	nmaReplicationData := nmaStartReplicationRequestData{}
	nmaReplicationData.DBName = options.DBName
	nmaReplicationData.ExcludePattern = options.ExcludePattern
	nmaReplicationData.IncludePattern = options.IncludePattern
	nmaReplicationData.TableOrSchemaName = options.TableOrSchemaName
	nmaReplicationData.Username = options.UserName
	nmaReplicationData.Password = options.Password
	nmaReplicationData.TargetDBName = options.TargetDB.DBName
	nmaReplicationData.TargetHost = initiatorTargetHost
	nmaReplicationData.TargetNamespace = options.TargetNamespace
	nmaReplicationData.TargetUserName = options.TargetDB.UserName
	nmaReplicationData.TargetPassword = options.TargetDB.Password
	nmaReplicationData.TLSConfig = options.SourceTLSConfig

	nmaStartReplicationOp, err := makeNMAReplicationStartOp(options.Hosts, options.usePassword, targetUsePassword,
		&nmaReplicationData, vdb)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaStartReplicationOp,
	)

	return instructions, nil
}

func (vcc VClusterCommands) produceGetNewTransactionIDInstructions(options *VReplicationDatabaseOptions,
	vdb *VCoordinationDatabase, targetUsePassword bool, transactionIDs *[]int64,
	asyncReplicationTransactionID *int64) ([]clusterOp, error) {
	var instructions []clusterOp

	nmaPollReplicationStatusOp, err := makeNMAPollReplicationStatusOp(&options.TargetDB, targetUsePassword,
		options.SandboxName, vdb, transactionIDs, asyncReplicationTransactionID)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaPollReplicationStatusOp,
	)

	return instructions, nil
}

// Perform synchronous database replication
func (vcc VClusterCommands) replicateDatabaseSync(options *VReplicationDatabaseOptions,
	vdb *VCoordinationDatabase) error {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// produce database replication instructions
	instructions, err := vcc.produceSyncDBReplicationInstructions(options, vdb)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		if strings.Contains(runError.Error(), "EnableConnectCredentialForwarding is false") {
			runError = fmt.Errorf("target database authentication failed, need to do one of the following things: " +
				"1. provide tlsconfig or target username with password " +
				"2. set EnableConnectCredentialForwarding to True in source database using vsql " +
				"3. configure a Trust Authentication in target database using vsql")
		}
		return fmt.Errorf("fail to replicate database: %w", runError)
	}

	return nil
}

// The generated instructions will later perform the following operations necessary for synchronous replication:
//   - Disallow multiple namespaces
//   - Replicate database (synchronous)
func (vcc VClusterCommands) produceSyncDBReplicationInstructions(options *VReplicationDatabaseOptions,
	vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp

	// need username for https operations in source database
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}

	// verify the username for connecting to the target database
	targetUsePassword := false
	if options.TargetDB.Password != nil {
		targetUsePassword = true
		if options.TargetDB.UserName == "" {
			username, e := util.GetCurrentUsername()
			if e != nil {
				return instructions, e
			}
			options.TargetDB.UserName = username
		}
		vcc.Log.Info("Current target username", "username", options.TargetDB.UserName)
	}

	initiatorTargetHost := getInitiator(options.TargetDB.Hosts)

	httpsDisallowMultipleNamespacesOp, err := makeHTTPSDisallowMultipleNamespacesOp(options.Hosts,
		options.usePassword, options.UserName, options.Password, options.SandboxName, vdb)
	if err != nil {
		return instructions, err
	}

	httpsStartReplicationOp, err := makeHTTPSStartReplicationOp(options.DBName, options.Hosts, options.usePassword,
		options.UserName, options.Password, targetUsePassword, &options.TargetDB, initiatorTargetHost,
		options.SourceTLSConfig, options.SandboxName, vdb)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsDisallowMultipleNamespacesOp,
		&httpsStartReplicationOp,
	)

	return instructions, nil
}
