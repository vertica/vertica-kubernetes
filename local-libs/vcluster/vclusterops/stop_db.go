/*
 (c) Copyright [2023-2025] Open Text.
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
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type ActiveSessionDetails struct {
	SessionID      string `json:"session_id"`
	Username       string `json:"user_name"`
	ClientHostname string `json:"client_hostname"`
}

// Thrown when running "stop_db --if-no-users" while users are connected
type ActiveSessionsError struct {
	Sessions []ActiveSessionDetails
}

func (e *ActiveSessionsError) Error() string {
	return "fail to stop database: cannot shutdown while users are connected"
}

type VStopDatabaseOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	/* part 2: eon db info */
	DrainSeconds *int   // time in seconds to wait for database users' disconnection
	SandboxName  string // Stop db on given sandbox
	MainCluster  bool   // Stop db on main cluster only
	/* part 3: hidden info */
	CheckUserConn bool // whether check user connection
	ForceKill     bool // whether force kill connections
}

func VStopDatabaseOptionsFactory() VStopDatabaseOptions {
	options := VStopDatabaseOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VStopDatabaseOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VStopDatabaseOptions) validateRequiredOptions(log vlog.Printer) error {
	err := options.validateBaseOptions(StopDBCmd, log)
	if err != nil {
		return err
	}

	return nil
}

func (options *VStopDatabaseOptions) validateEonOptions(log vlog.Printer) error {
	if options.SandboxName != "" && options.MainCluster {
		return fmt.Errorf("Error: cannot use both --sandbox and --main-cluster-only options together ")
	}

	// if db is enterprise db and we see --drain-seconds, we will ignore it
	if !options.IsEon {
		if options.DrainSeconds != nil {
			log.PrintInfo("Notice: --drain-seconds option will be ignored because database is in enterprise mode." +
				" Connection draining is only available in eon mode.")
		}
		options.DrainSeconds = nil
	} else if options.isShutdownWithDrain() && options.DrainSeconds == nil {
		// if db is eon db and we do not see --drain-seconds, we will set it to 60 seconds (default value)
		options.DrainSeconds = new(int)
		*options.DrainSeconds = util.DefaultDrainSeconds
	}
	return nil
}

func (options *VStopDatabaseOptions) validateExtraOptions() error {
	if options.SandboxName != "" {
		err := util.ValidateSandboxName(options.SandboxName)
		if err != nil {
			return err
		}
	}
	return nil
}

func (options *VStopDatabaseOptions) validateParseOptions(log vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(log)
	if err != nil {
		return err
	}
	// batch 2: validate eon params
	err = options.validateEonOptions(log)
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

// resolve hostnames to be IPs
func (options *VStopDatabaseOptions) analyzeOptions() (err error) {
	// resolve RawHosts to be IP addresses
	if len(options.RawHosts) > 0 {
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VStopDatabaseOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := options.validateParseOptions(log); err != nil {
		return err
	}
	return options.analyzeOptions()
}

func (options *VStopDatabaseOptions) isShutdownWithDrain() bool {
	return !options.ForceKill && !options.CheckUserConn
}

func (vcc VClusterCommands) VStopDatabase(options *VStopDatabaseOptions) error {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	err := options.validateUserName(vcc.Log)
	if err != nil {
		return err
	}

	// validate and analyze all options
	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// get vdb and check requirements
	vdb := makeVCoordinationDatabase()
	if options.MainCluster {
		vcc.Log.Info("getting vdb info from main cluster")
		err = vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	} else {
		vcc.Log.Info("getting vdb info for sandbox")
		err = vcc.getDeepVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	}
	if err != nil {
		vcc.LogError(err, "failed to get vdb from running db")
	} else {
		// stop_db is aborted if requirements are not met.
		err = options.checkStopDBRequirements(&vdb)
		if err != nil {
			return err
		}
	}

	options.setAllHosts(&vdb)

	activeSessions := []ActiveSessionDetails{}

	instructions, err := vcc.produceStopDBInstructions(options, &activeSessions)
	if err != nil {
		return fmt.Errorf("fail to production instructions: %w", err)
	}
	// Create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.runInSandbox(vcc.GetLog(), &vdb, options.SandboxName)
	if runError != nil {
		if errors.Is(runError, ErrActiveSessions) {
			return &ActiveSessionsError{Sessions: activeSessions}
		}

		return fmt.Errorf("fail to stop database: %w", runError)
	}

	return nil
}

// produceStopDBInstructions will build a list of instructions to execute for
// the stop db operation.
//
// The generated instructions will later perform the following operations necessary
// for a successful stop_db:
//   - Get up nodes through https call
//   - Sync catalog through the first up node
//   - Stop db through the first up node
//   - Check there is not any database running
func (vcc *VClusterCommands) produceStopDBInstructions(options *VStopDatabaseOptions,
	activeSessions *[]ActiveSessionDetails) ([]clusterOp, error) {
	var instructions []clusterOp

	// when password is specified, we will use username/password to call https endpoints
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}

	if options.CheckUserConn {
		nmaCheckActiveSessionOp, err := vcc.produceCheckNMAActiveSessionsInstruction(
			options, activeSessions)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &nmaCheckActiveSessionOp)
	}

	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesWithSandboxOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password, StopDBCmd, options.SandboxName, options.MainCluster)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &httpsGetUpNodesOp)
	if options.IsEon && options.IfSyncCatalog {
		httpsSyncCatalogOp, e := makeHTTPSSyncCatalogOpWithoutHosts(options.usePassword, options.UserName, options.Password, StopDBSyncCat)
		if e != nil {
			return instructions, e
		}
		instructions = append(instructions, &httpsSyncCatalogOp)
	} else if options.IfSyncCatalog {
		vcc.Log.PrintInfo("Skipping sync catalog for an enterprise database")
	}

	httpsStopDBOp, err := makeHTTPSStopDBOp(options.usePassword, options.UserName, options.Password, options.DrainSeconds,
		options.SandboxName, options.MainCluster, options.IsEon, options.ForceKill)
	if err != nil {
		return instructions, err
	}

	httpsCheckDBRunningOp, err := makeHTTPSCheckRunningDBWithSandboxOp(options.Hosts,
		options.usePassword, options.UserName, options.SandboxName, options.MainCluster, options.Password, StopDB, options.DBName)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsStopDBOp,
		&httpsCheckDBRunningOp,
	)

	return instructions, nil
}

func (vcc *VClusterCommands) produceCheckNMAActiveSessionsInstruction(options *VStopDatabaseOptions,
	activeSessions *[]ActiveSessionDetails) (nmaCheckActiveSessionsOp, error) {
	nmaCheckActiveSessionsOp := nmaCheckActiveSessionsOp{}
	vdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return nmaCheckActiveSessionsOp, err
	}
	// Get up hosts
	hosts := options.Hosts
	if options.MainCluster {
		hosts = vdb.filterUpHostListBySandbox(hosts, util.MainClusterSandbox)
	} else {
		hosts = vdb.filterUpHostListBySandbox(hosts, options.SandboxName)
	}
	if len(hosts) == 0 {
		return nmaCheckActiveSessionsOp, fmt.Errorf("found no UP nodes for capture workload")
	}

	// Get initiator host to send NMA requests to
	initiator, err := getInitiatorHost(vdb.PrimaryUpNodes, []string{} /* skip hosts */)
	if err != nil {
		return nmaCheckActiveSessionsOp, err
	}
	initiatorHost := []string{initiator}

	return makeNMACheckActiveSessionsOp(initiatorHost,
		options.DBName, options.UserName, options.Password,
		activeSessions, true /* assertNoActiveSessions*/)
}

// checkStopDBRequirements validates any stop_db requirements. It will
// return an error if a requirement isn't met.
func (options *VStopDatabaseOptions) checkStopDBRequirements(vdb *VCoordinationDatabase) error {
	// if stop db on the whole cluster, at least one UP main cluster host in the host list
	if options.SandboxName == "" && !options.MainCluster {
		hasMainClusterHost := false
		for _, host := range options.Hosts {
			vnode, ok := vdb.HostNodeMap[host]
			if ok && vnode.Sandbox == "" {
				hasMainClusterHost = true
				break
			}
		}
		if !hasMainClusterHost {
			return fmt.Errorf("should specify at least one UP main cluster host in the host list")
		}
	}
	return nil
}

// setAllHosts set the list of all nodes. It will contain the following list of hosts:
//   - Case 1: users specified to stop main cluster only and the currently processing node belongs to the main cluster.
//   - Case 2: users did not specify only stopping main cluster:
//   - case 2.1: users did not specify a specific sandbox, in this case,
//     it's stopping the entire database, we include all nodes of the database.
//   - case 2.2: users specified a specific sandbox, in this case, we only include the nodes in the specific sandbox.
func (options *VStopDatabaseOptions) setAllHosts(vdb *VCoordinationDatabase) {
	allHosts := []string{}
	for h, vnode := range vdb.HostNodeMap {
		if (options.MainCluster && vnode.Sandbox == util.MainClusterSandbox) ||
			(!options.MainCluster && (options.SandboxName == util.MainClusterSandbox || options.SandboxName == vnode.Sandbox)) {
			allHosts = append(allHosts, h)
		}
	}
	if len(allHosts) > 0 {
		options.Hosts = allHosts
	}
}
