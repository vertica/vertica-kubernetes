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
	"fmt"
	"time"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type UpgradeVerticaPhase int

const (
	UpgradeVerticaPhase1 UpgradeVerticaPhase = iota
	UpgradeVerticaPhase2
	UpgradeVerticaPhase3

	disableNonReplicatableQueries = "disablenonreplicatablequeries"
)

type VerticaUpgradePostSandboxHook interface {
	RunPostSandboxHook(subcluster string, vcc ClusterCommands)
}

type VUpgradeVerticaOptions struct {
	DatabaseOptions
	Phase          UpgradeVerticaPhase
	PollingTimeout int
	// phase 1 and 2
	SandboxName   string
	RedirectHosts string
	// phase 1 only
	SandboxSubclusters []string
	PostSandboxHook    VerticaUpgradePostSandboxHook
	// phase 2 only
	ReplicateTLSConfig string
	// phase 3 only
	PrimarySubclusters []string
}

func VUpgradeVerticaOptionsFactory() VUpgradeVerticaOptions {
	options := VUpgradeVerticaOptions{}
	// set default values to the params
	options.setDefaultValues()
	return options
}

func (options *VUpgradeVerticaOptions) validateRequiredOptions(logger vlog.Printer) error {
	if err := options.validateBaseOptions(UpgradeVerticaCmd, logger); err != nil {
		return err
	}

	switch options.Phase {
	case UpgradeVerticaPhase1:
		if len(options.SandboxSubclusters) == 0 {
			return fmt.Errorf("must specify sandbox subclusters for phase 1 of Vertica upgrade")
		}
		if options.SandboxName == "" {
			return fmt.Errorf("must specify sandbox name for phase 1 of Vertica upgrade")
		}
		if options.RedirectHosts == "" {
			return fmt.Errorf("must specify redirect destination for phase 1 of Vertica upgrade")
		}
	case UpgradeVerticaPhase2:
		if options.SandboxName == "" {
			return fmt.Errorf("must specify sandbox name for phase 2 of Vertica upgrade")
		}
		if options.RedirectHosts == "" {
			return fmt.Errorf("must specify redirect destination for phase 2 of Vertica upgrade")
		}
	case UpgradeVerticaPhase3:
		if len(options.PrimarySubclusters) == 0 {
			return fmt.Errorf("must specify primary subclusters for phase 3 of Vertica upgrade")
		}
	}

	return nil
}

func (options *VUpgradeVerticaOptions) validateParseOptions(log vlog.Printer) error {
	// validate required parameters
	err := options.validateRequiredOptions(log)
	if err != nil {
		return err
	}

	err = options.validateAuthOptions(UpgradeVerticaCmd.CmdString(), log)
	if err != nil {
		return err
	}
	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VUpgradeVerticaOptions) analyzeOptions() (err error) {
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

func (options *VUpgradeVerticaOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := options.validateParseOptions(log); err != nil {
		return err
	}
	if err := options.analyzeOptions(); err != nil {
		return err
	}
	if err := options.setUsePassword(log); err != nil {
		return err
	}
	return options.validateUserName(log)
}

func (vcc VClusterCommands) VUpgradeVertica(options *VUpgradeVerticaOptions) error {
	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	switch options.Phase {
	case UpgradeVerticaPhase1:
		err = vcc.doUpgradeVerticaPhase1(options)
	case UpgradeVerticaPhase2:
		err = vcc.doUpgradeVerticaPhase2(options)
	case UpgradeVerticaPhase3:
		err = vcc.doUpgradeVerticaPhase3(options)
	}

	return err
}

// create sandbox for upgrade
func (vcc *VClusterCommands) doUpgradeVerticaPhase1(options *VUpgradeVerticaOptions) error {
	// drain to-be-sandboxed subclusters
	for i := range options.SandboxSubclusters {
		opts := VManageConnectionDrainingOptionsFactory()
		opts.DatabaseOptions = options.DatabaseOptions
		opts.Action = ActionPause
		opts.SCName = options.SandboxSubclusters[i]
		if err := vcc.VManageConnectionDraining(&opts); err != nil {
			return err
		}
		opts.Action = ActionRedirect
		opts.RedirectHostname = options.RedirectHosts
		if err := vcc.VManageConnectionDraining(&opts); err != nil {
			return err
		}
	}

	// wait for redirect to finish
	pollOpts := VPollConnectionDrainingOptionsFactory()
	pollOpts.DatabaseOptions = options.DatabaseOptions
	pollOpts.Subclusters = options.SandboxSubclusters
	pollOpts.Timeout = options.PollingTimeout
	pollOpts.AllSessions = true
	if err := vcc.VPollConnectionDraining(&pollOpts); err != nil {
		vcc.DisplayWarning("Failed to wait for all connections to be drained, remaining connections will be killed")
	}

	// disable non-replicable queries until upgrade is complete
	setConfigOpts := VSetConfigurationParameterOptionsFactory()
	setConfigOpts.DatabaseOptions = options.DatabaseOptions
	setConfigOpts.ConfigParameter = disableNonReplicatableQueries
	setConfigOpts.Value = "1"
	if err := vcc.VSetConfigurationParameters(&setConfigOpts); err != nil {
		return fmt.Errorf("failed to set DisableNonReplicatableQueries: %w; "+
			"please make sure DisableNonReplicatableQueries is unset and try again", err)
	}

	// sandbox subclusters then stop them to allow user to upgrade installed vertica
	for i, sc := range options.SandboxSubclusters {
		sbOpts := VSandboxOptionsFactory()
		sbOpts.DatabaseOptions = options.DatabaseOptions
		sbOpts.SCName = sc
		sbOpts.SandboxName = options.SandboxName
		sbOpts.ForUpgrade = true
		if err := vcc.VSandbox(&sbOpts); err != nil {
			if i == 0 {
				return fmt.Errorf("failed to create sandbox '%s': %w; please retry command", options.SandboxName, err)
			}
			return fmt.Errorf("failed to add subcluster '%s' to sandbox '%s': %w; "+
				"please add remaining subclusters to sandbox and shut it down before continuing online upgrade",
				sc, options.SandboxName, err)
		}
		if options.PostSandboxHook != nil {
			options.PostSandboxHook.RunPostSandboxHook(sc, vcc)
		}

		pollOpts := VPollSubclusterStateOptionsFactory()
		pollOpts.DatabaseOptions = options.DatabaseOptions
		pollOpts.Hosts = sbOpts.SCHosts
		pollOpts.SCName = sc
		pollOpts.Timeout = options.PollingTimeout
		if err := vcc.VPollSubclusterState(&pollOpts); err != nil {
			vcc.LogError(err, "Failed to wait for sandboxed subcluster to come up")
			return fmt.Errorf("failed to wait for sandboxed subcluster '%s' to come up: %w; "+
				"please add remaining subclusters to sandbox and shut it down before continuing online upgrade", sc, err)
		}
	}

	stopDBOpts := VStopDatabaseOptionsFactory()
	stopDBOpts.DatabaseOptions = options.DatabaseOptions
	stopDBOpts.SandboxName = options.SandboxName
	if err := vcc.VStopDatabase(&stopDBOpts); err != nil {
		return fmt.Errorf("failed to stop sandbox '%s': %w; please stop sandbox before continuing online upgrade",
			options.SandboxName, err)
	}
	return nil
}

func (vcc *VClusterCommands) upgradeHelperRunStartDB(options *VUpgradeVerticaOptions) (*VCoordinationDatabase, error) {
	vdb := VCoordinationDatabase{}
	if err := vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions); err != nil {
		return nil, err
	}

	startDBOpts := VStartDatabaseOptionsFactory()
	startDBOpts.DatabaseOptions = options.DatabaseOptions
	startDBOpts.RawHosts = []string{}
	for _, h := range vdb.HostList {
		n := vdb.HostNodeMap[h]
		if options.SandboxName == n.Sandbox {
			startDBOpts.RawHosts = append(startDBOpts.RawHosts, n.Address)
		}
	}
	startDBOpts.Hosts = []string{}
	startDBOpts.HostsInSandbox = true
	startDBOpts.MainCluster = false
	startDBOpts.Sandbox = options.SandboxName
	startDBOpts.StatePollingTimeout = options.PollingTimeout
	return vcc.VStartDatabase(&startDBOpts)
}

func (vcc *VClusterCommands) upgradeHelperReplicateRedirectState(options *VUpgradeVerticaOptions) error {
	// only insert rows not already existing in the sandbox
	getRedirStateOpts := VGetRedirectStateOptionsFactory()
	getRedirStateOpts.DatabaseOptions = options.DatabaseOptions
	getRedirStateOpts.Sandbox = options.SandboxName
	sbState, err := vcc.VGetRedirectState(&getRedirStateOpts)
	if err != nil {
		return err
	}
	getRedirStateOpts.Sandbox = util.MainClusterSandbox
	getRedirStateOpts.ExcludeIDs = []string{}
	for _, row := range sbState {
		getRedirStateOpts.ExcludeIDs = append(getRedirStateOpts.ExcludeIDs, row.ID)
	}
	setRedirStateOpts := VSetRedirectStateOptionsFactory()
	setRedirStateOpts.DatabaseOptions = options.DatabaseOptions
	setRedirStateOpts.Sandbox = options.SandboxName
	if setRedirStateOpts.Rows, err = vcc.VGetRedirectState(&getRedirStateOpts); err != nil {
		return err
	}
	return vcc.VSetRedirectState(&setRedirStateOpts)
}

func (vcc *VClusterCommands) upgradeHelperPromoteSandbox(options *VUpgradeVerticaOptions, vdb *VCoordinationDatabase) error {
	// we just shut down the main so it might take a bit to propagate, retry until success or timeout
	sbConvertOpts := VPromoteSandboxToMainFactory()
	sbConvertOpts.DatabaseOptions = options.DatabaseOptions
	sbConvertOpts.DatabaseOptions.Hosts = vdb.filterUpHostListBySandbox(vdb.HostList, options.SandboxName)
	sbConvertOpts.SandboxName = options.SandboxName
	begin := time.Now()
	err := vcc.VPromoteSandboxToMain(&sbConvertOpts)
	for err != nil && time.Now().Compare(begin.Add(time.Duration(options.PollingTimeout)*time.Second)) < 0 {
		sleepDuration := 5 * time.Second
		time.Sleep(sleepDuration)
		err = vcc.VPromoteSandboxToMain(&sbConvertOpts)
	}

	if err != nil {
		return err
	}

	mainHosts := vdb.filterUpHostListBySandbox(vdb.HostList, util.MainClusterSandbox)
	sbHosts := vdb.filterUpHostListBySandbox(vdb.HostList, options.SandboxName)
	// Clean catalog dirs
	nmaDeleteDirsOp, err := makeNMADeleteDirsSandboxOp(mainHosts, true /* force delete? */, true /* is unsandbox op? */)
	if err != nil {
		return err
	}
	instructions := []clusterOp{&nmaDeleteDirsOp}
	mainCluster := util.MainClusterSandbox
	produceTransferConfigOps(&instructions, sbHosts, mainHosts, vdb, &mainCluster)
	clusterOpEngine := makeClusterOpEngine(instructions, options)
	execContext := makeOpEngineExecContext(vcc.Log)
	for _, node := range vdb.HostNodeMap {
		// makeHTTPSGetClusterInfoOp fails if no main cluster nodes are up and we just killed them
		// so, fill this in manually
		execContext.scNodesInfo = append(execContext.scNodesInfo, NodeInfo{
			Address:     node.Address,
			Name:        node.Name,
			State:       node.State,
			CatalogPath: node.CatalogPath,
			Subcluster:  node.Subcluster,
			Sandbox:     node.Sandbox,
			IsPrimary:   node.IsPrimary,
			Version:     node.Version,
			IsCompute:   node.IsComputeNode,
		})
	}

	clusterOpEngine.execContext = &execContext
	return clusterOpEngine.runWithExecContext(vcc.Log, &execContext)
}

// redirect connections to sandbox and shut down old main cluster
//
//nolint:funlen,gocyclo
func (vcc *VClusterCommands) doUpgradeVerticaPhase2(options *VUpgradeVerticaOptions) error {
	restartNmaOpts := VRestartNMAOptionsFactory()
	restartNmaOpts.DatabaseOptions = options.DatabaseOptions
	restartNmaOpts.Sandbox = options.SandboxName
	restartNmaOpts.PollingTimeout = options.PollingTimeout
	if err := vcc.VRestartNMA(&restartNmaOpts); err != nil {
		return fmt.Errorf("failed to restart Node Management Agent on sandbox nodes: %w; "+
			"please make sure the NMA is running on all nodes and retry command", err)
	}

	vdb, err := vcc.upgradeHelperRunStartDB(options)
	if err != nil {
		return fmt.Errorf("failed to start sandbox: %w; "+
			"please make sure the NMA is running on all nodes and retry command", err)
	}

	// enable non-replicable queries in sandbox
	setConfigOpts := VSetConfigurationParameterOptionsFactory()
	setConfigOpts.DatabaseOptions = options.DatabaseOptions
	setConfigOpts.Sandbox = options.SandboxName
	setConfigOpts.ConfigParameter = disableNonReplicatableQueries
	setConfigOpts.Value = util.NullVal
	if err := vcc.VSetConfigurationParameters(&setConfigOpts); err != nil {
		return fmt.Errorf("failed to clear DisableNonReplicatableQueries database parameter in the sandbox: %w; "+
			"please shut down sandbox and retry command", err)
	}

	// pause connections in sandbox (no need to poll; there shouldn't be any connections)
	sbPauseOpts := VManageConnectionDrainingOptionsFactory()
	sbPauseOpts.DatabaseOptions = options.DatabaseOptions
	sbPauseOpts.Sandbox = options.SandboxName
	sbPauseOpts.Action = ActionPause
	if err := vcc.VManageConnectionDraining(&sbPauseOpts); err != nil {
		return fmt.Errorf("failed to pause connections to sandbox: %w; "+
			"please shut down sandbox and retry command", err)
	}

	// pause connections in main (no need to poll now, going to do other work first then wait)
	mainPauseOpts := VManageConnectionDrainingOptionsFactory()
	mainPauseOpts.DatabaseOptions = options.DatabaseOptions
	mainPauseOpts.Action = ActionPause
	if err := vcc.VManageConnectionDraining(&mainPauseOpts); err != nil {
		return fmt.Errorf("failed to pause connections to main cluster: %w; "+
			"please shut down sandbox and retry command", err)
	}

	// replicate from main cluster to sandbox
	replicateOpts := VReplicationDatabaseFactory()
	replicateOpts.DatabaseOptions = options.DatabaseOptions
	replicateOpts.TargetDB = options.DatabaseOptions
	replicateOpts.TargetDB.Hosts = vdb.filterUpHostListBySandbox(replicateOpts.TargetDB.Hosts, options.SandboxName)
	replicateOpts.SourceTLSConfig = options.ReplicateTLSConfig
	replicateOpts.Async = false
	if _, err := vcc.VReplicateDatabase(&replicateOpts); err != nil {
		return fmt.Errorf("failed to replicate from main cluster to sandbox: %w; "+
			"please shut down sandbox and retry command", err)
	}

	if err := vcc.upgradeHelperReplicateRedirectState(options); err != nil {
		return fmt.Errorf("failed to replicate from main cluster to sandbox: %w; "+
			"please shut down sandbox and retry command", err)
	}

	*vdb = VCoordinationDatabase{}
	if err := vcc.getVDBFromRunningDB(vdb, &options.DatabaseOptions); err != nil {
		return fmt.Errorf("%w; please shut down sandbox and retry command", err)
	}

	// wait for pause to finish
	pollOpts := VPollConnectionDrainingOptionsFactory()
	pollOpts.DatabaseOptions = options.DatabaseOptions
	scStatus := vdb.getScStatus()
	for sc, status := range scStatus {
		if status.Sandbox == util.MainClusterSandbox {
			pollOpts.Subclusters = append(pollOpts.Subclusters, sc)
		}
	}
	pollOpts.Timeout = options.PollingTimeout
	pollOpts.AllSessions = false // only wait for sessions to be paused
	if err := vcc.VPollConnectionDraining(&pollOpts); err != nil {
		vcc.DisplayWarning("Failed to wait for all connections to be paused, data may be lost during upgrade")
	}

	// replicate again now that we've finished all that
	if _, err := vcc.VReplicateDatabase(&replicateOpts); err != nil {
		return fmt.Errorf("failed to replicate from main cluster to sandbox: %w; "+
			"please shut down sandbox and retry command", err)
	}

	// unpause sandbox
	sbPauseOpts.Action = ActionResume
	if err := vcc.VManageConnectionDraining(&sbPauseOpts); err != nil {
		return fmt.Errorf("failed to resume connections sandbox: %w; "+
			"please shut down sandbox and retry command", err)
	}

	// TODO: refine this, allowing users to specify a map from source to redir dest
	redirOpts := VManageConnectionDrainingOptionsFactory()
	redirOpts.DatabaseOptions = options.DatabaseOptions
	redirOpts.Action = ActionRedirect
	redirOpts.RedirectHostname = options.RedirectHosts
	if err := vcc.VManageConnectionDraining(&redirOpts); err != nil {
		return fmt.Errorf("failed to redirect connections from main cluster to sandbox: %w; "+
			"please shut down sandbox and retry command", err)
	}

	pollOpts.AllSessions = true // wait for all sessions to be redirected
	if err := vcc.VPollConnectionDraining(&pollOpts); err != nil {
		vcc.DisplayWarning("Failed to wait for all connections to be drained, remaining connections will be killed")
	}

	zero := 0
	stopDBOpts := VStopDatabaseOptionsFactory()
	stopDBOpts.DatabaseOptions = options.DatabaseOptions
	stopDBOpts.MainCluster = true
	stopDBOpts.DrainSeconds = &zero
	if err := vcc.VStopDatabase(&stopDBOpts); err != nil {
		return fmt.Errorf("failed to stop main cluster: %w; "+
			"please shut down main cluster and promote sandbox to main before continuing with online upgrade", err)
	}

	if err := vcc.upgradeHelperPromoteSandbox(options, vdb); err != nil {
		return fmt.Errorf("failed to promote sandbox: %w; "+
			"please ensure sandbox has been promoted before continuing with online upgrade", err)
	}

	return nil
}

func (vcc *VClusterCommands) upgradeVerticaHelperPromoteDemote(scStatus map[string]scStatus, options *VUpgradeVerticaOptions) error {
	promoteDemoteOpts := VPromoteDemoteFactory()
	promoteDemoteOpts.DatabaseOptions = options.DatabaseOptions
	promoteDemoteOpts.SCType = Secondary
	for _, sc := range options.PrimarySubclusters {
		promoteDemoteOpts.SCName = sc
		if !scStatus[sc].IsPrimary {
			if err := vcc.VAlterSubclusterType(&promoteDemoteOpts); err != nil {
				return err
			}
		}
		delete(scStatus, sc)
	}

	promoteDemoteOpts.SCType = Primary
	for sc := range scStatus {
		promoteDemoteOpts.SCName = sc
		if scStatus[sc].IsPrimary {
			if err := vcc.VAlterSubclusterType(&promoteDemoteOpts); err != nil {
				return err
			}
		}
	}
	return nil
}

// start all subclusters and promote/demote accordingly
func (vcc *VClusterCommands) doUpgradeVerticaPhase3(options *VUpgradeVerticaOptions) error {
	restartNmaOpts := VRestartNMAOptionsFactory()
	restartNmaOpts.DatabaseOptions = options.DatabaseOptions
	if err := vcc.VRestartNMA(&restartNmaOpts); err != nil {
		return fmt.Errorf("failed to restart Node Management Agent on all cluster nodes: %w; "+
			"please make sure the NMA is running on all nodes and retry command", err)
	}

	vdb := VCoordinationDatabase{}
	err := vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return err
	}

	vdb.filterMainClusterNodes()
	scStatus := vdb.getScStatus()

	for sc, status := range scStatus {
		if status.IsUp {
			continue
		}

		opts := VStartScOptionsFactory()
		opts.DatabaseOptions = options.DatabaseOptions
		opts.StatePollingTimeout = options.PollingTimeout
		opts.SCName = sc
		if vdb, err = vcc.VStartSubcluster(&opts); err != nil {
			return fmt.Errorf("failed to start subcluster %s: %w; "+
				"please make sure all main cluster subclusters are running and retry command", sc, err)
		}
	}

	if err := vcc.upgradeVerticaHelperPromoteDemote(scStatus, options); err != nil {
		return fmt.Errorf("failed to promote/demote subclusters: %w; please promote or demote subclusters manually", err)
	}

	return nil
}
