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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
	"golang.org/x/exp/maps"
)

const defaultKsafety = 1

type VReIPOptions struct {
	DatabaseOptions

	// re-ip list
	ReIPList    []ReIPInfo
	SandboxName string // sandbox name or empty string for main cluster, all nodes must in same sandbox for one re_ip action
	/* hidden option */

	// whether trim re-ip list based on the catalog info
	TrimReIPList bool
	// perform an additional HTTPS check (checkRunningDB operation) to verify that the database is running.
	// This is useful when Re-IP should only be applied to down db.
	CheckDBRunning bool
	// optional ksafety parameter with default value of 1
	Ksafety int

	// hidden option
	newAddresses           []string
	ForceLoadRemoteCatalog bool
}

func VReIPFactory() VReIPOptions {
	options := VReIPOptions{}
	// set default values to the params
	options.setDefaultValues()
	options.TrimReIPList = false
	options.SandboxName = util.MainClusterSandbox
	options.Ksafety = defaultKsafety
	return options
}

func (options *VReIPOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(ReIPCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VReIPOptions) validateExtraOptions() error {
	err := util.ValidateRequiredAbsPath(options.CatalogPrefix, "catalog path")
	if err != nil {
		return err
	}

	if options.CommunalStorageLocation != "" {
		return util.ValidateCommunalStorageLocation(options.CommunalStorageLocation)
	}

	return nil
}

func (options *VReIPOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required parameters
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	// batch 2: validate all other params
	err = options.validateExtraOptions()
	if err != nil {
		return err
	}
	return nil
}

func (options *VReIPOptions) analyzeOptions() error {
	if len(options.RawHosts) > 0 {
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}
	return nil
}

func (options *VReIPOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	if err := options.analyzeOptions(); err != nil {
		return err
	}

	// the re-ip list must not be empty
	if len(options.ReIPList) == 0 {
		return errors.New("the re-ip list is not provided")
	}

	// address check
	ipv6 := options.IPv6
	nodeAddresses := make(map[string]struct{})
	for _, info := range options.ReIPList {
		// the addresses must be valid IPs
		if info.NodeAddress != "" {
			if info.NodeAddress == util.UnboundedIPv4 || info.NodeAddress == util.UnboundedIPv6 {
				return errors.New("the re-ip list should not contain unbound addresses")
			}
		}

		if err := util.AddressCheck(info.TargetAddress, ipv6); err != nil {
			return err
		}
		if info.TargetControlAddress != "" {
			if err := util.AddressCheck(info.TargetControlAddress, ipv6); err != nil {
				return err
			}
		}
		if info.TargetControlBroadcast != "" {
			if err := util.AddressCheck(info.TargetControlBroadcast, ipv6); err != nil {
				return err
			}
		}

		// the target node addresses in the re-ip list must not be empty or duplicate
		addr := info.TargetAddress
		if addr == "" {
			return errors.New("the new node address should not be empty")
		}
		if _, ok := nodeAddresses[addr]; ok {
			return fmt.Errorf("the provided node address %s is duplicate", addr)
		}
		nodeAddresses[addr] = struct{}{}
	}
	return nil
}

// VReIP changes the node address, control address, and control broadcast for a node.
// It returns any error encountered.
func (vcc VClusterCommands) VReIP(options *VReIPOptions) error {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// VER-93369 may improve this if the CLI knows which nodes are primary
	// from the config file
	var pVDB *VCoordinationDatabase
	// retrieve database information from cluster_config.json for Eon databases
	if options.IsEon {
		const warningMsg = " for an Eon database, re_ip after revive_db could fail " +
			util.DBInfo
		if options.CommunalStorageLocation != "" {
			vdb, e := options.getVDBFromSandboxWhenDBIsDown(vcc, options.SandboxName)
			if e != nil {
				// show a warning message if we cannot get VDB from a down database
				vcc.Log.PrintWarning(util.CommStorageFail + warningMsg)
			}
			pVDB = &vdb
		} else {
			// When communal storage location is missing, we only log a debug message
			// because re-ip only fails in between revive_db and first start_db.
			// We should not ran re-ip in that case because revive_db has already done the re-ip work.
			vcc.Log.V(1).Info(util.CommStorageLoc + warningMsg)
		}
	}

	// for debugging only, once --force-load-remote-catalog is set,
	// we will skip the regular re-ip but directly load remote catalog
	if options.ForceLoadRemoteCatalog {
		return vcc.loadRemoteCatalogPostReip(options, nil)
	}

	// produce re-ip instructions
	instructions, err := vcc.produceReIPInstructions(options, pVDB)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	if options.SandboxName == util.MainClusterSandbox {
		vcc.LogInfo("Re-IP the main cluster")
		runError := clusterOpEngine.run(vcc.Log)
		if runError != nil {
			return fmt.Errorf("fail to re-ip: %w", runError)
		}
	} else {
		vcc.LogInfo("Re-IP the sandbox", "sandbox", options.SandboxName)
		runError := clusterOpEngine.runInSandbox(vcc.Log, pVDB, options.SandboxName)
		if runError != nil {
			return fmt.Errorf("fail to re-ip: %w", runError)
		}
	}

	// cache NMA VDB for the extra steps
	nmaVdb := clusterOpEngine.execContext.nmaVDatabase
	// if re-ip failed due to quorum check, update the node catalog by loading remote catalog from communal storage on primary nodes
	if clusterOpEngine.execContext.quorumLost && options.IsEon {
		vcc.LogInfo("Quorum check failed, run re-ip by loading remote catalog from communal storage")
		return vcc.loadRemoteCatalogPostReip(options, &nmaVdb)
	}

	return nil
}

func (vcc VClusterCommands) loadRemoteCatalogPostReip(options *VReIPOptions, nmaVdb *nmaVDatabase) error {
	// get the old vdb either from the communal storage or from the catalog editor
	var oldVdb VCoordinationDatabase
	if nmaVdb == nil {
		const warningMsg = " for an Eon database, re-ip by loading remote catalog could fail " +
			util.DBInfo
		vdbFromCommunal, err := options.getVDBFromSandboxWhenDBIsDown(vcc, options.SandboxName)
		if err != nil {
			vcc.Log.PrintWarning(util.CommStorageFail + warningMsg)
		}
		oldVdb = vdbFromCommunal
	} else {
		populateVdbFromNMAVdb(&oldVdb, nmaVdb)
	}
	oldVdb.CommunalStorageLocation = options.CommunalStorageLocation

	extraStepInstructions, newVdb, err := vcc.produceExtraReIPInstructions(options, &oldVdb)
	if err != nil {
		return fmt.Errorf("fail to produce extra instructions, %w", err)
	}
	clusterOpEngine := makeClusterOpEngine(extraStepInstructions, options)

	if options.SandboxName == util.MainClusterSandbox {
		runError := clusterOpEngine.run(vcc.Log)
		if runError != nil {
			return fmt.Errorf("fail to run extra steps of re-ip: %w", runError)
		}
	} else {
		vcc.LogInfo("Load remote catalog for the sandbox", "sandbox", options.SandboxName)
		runError := clusterOpEngine.runInSandbox(vcc.Log, newVdb, options.SandboxName)
		if runError != nil {
			return fmt.Errorf("fail to run extra steps of re-ip: %w", runError)
		}
	}

	return nil
}

// The generated instructions will later perform the following operations necessary
// for a successful re_ip:
//   - Check NMA connectivity
//   - Read database info from catalog editor
//     (now we should know which hosts have the latest catalog)
//   - Run re-ip on the target nodes
func (vcc VClusterCommands) produceReIPInstructions(options *VReIPOptions, vdb *VCoordinationDatabase) ([]clusterOp, error) {
	var instructions []clusterOp

	if len(options.ReIPList) == 0 {
		return instructions, errors.New("the re-ip information is not provided")
	}

	hosts := options.Hosts

	nmaHealthOp := makeNMAHealthOp(hosts)
	// need username for https operations
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions, &nmaHealthOp)

	if options.CheckDBRunning {
		sandbox := options.SandboxName
		mainCluster := false
		if sandbox == util.MainClusterSandbox {
			mainCluster = true
		}
		checkDBRunningOp, err := makeHTTPSCheckRunningDBWithSandboxOp(hosts,
			options.usePassword, options.UserName, sandbox, mainCluster, options.Password, ReIP, options.DBName)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &checkDBRunningOp)
	}

	// get network profiles of the new addresses
	var newAddresses []string
	for _, info := range options.ReIPList {
		newAddresses = append(newAddresses, info.TargetAddress)
	}
	options.newAddresses = newAddresses
	nmaNetworkProfileOp := makeNMANetworkProfileOp(newAddresses)

	instructions = append(instructions, &nmaNetworkProfileOp)

	vdbWithPrimaryNodes := new(VCoordinationDatabase)
	// use a copy of vdb because we want to keep secondary nodes in vdb for next nmaReIPOP
	if vdb != nil {
		*vdbWithPrimaryNodes = *vdb
		vdbWithPrimaryNodes.filterPrimaryNodes()
	}
	// When we cannot get primary nodes' info from cluster_config.json, we will fetch it from NMA /nodes endpoint.
	if vdb == nil || len(vdbWithPrimaryNodes.HostNodeMap) == 0 {
		vdb = new(VCoordinationDatabase)
		nmaGetNodesInfoOp := makeNMAGetNodesInfoOp(options.Hosts, options.DBName, options.CatalogPrefix,
			false /* report all errors */, vdb)
		// read catalog editor to get hosts with latest catalog
		nmaReadCatEdOp, err := makeNMAReadCatalogEditorOpWithSandbox(vdb, options.SandboxName)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions,
			&nmaGetNodesInfoOp,
			&nmaReadCatEdOp,
		)
	} else {
		// read catalog editor to get hosts with latest catalog
		nmaReadCatEdOp, err := makeNMAReadCatalogEditorOpWithSandbox(vdbWithPrimaryNodes, options.SandboxName)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &nmaReadCatEdOp)
	}

	// re-ip
	// at this stage the re-ip info should either by provided by
	// the re-ip file (for vcluster CLI) or the Kubernetes operator
	nmaReIPOp := makeNMAReIPOp(options.ReIPList, vdb, options.TrimReIPList, options.Ksafety)
	instructions = append(instructions, &nmaReIPOp)

	return instructions, nil
}

// produceExtraReIPInstructions generates additional instructions to handle cases where the quorum check fails
func (vcc VClusterCommands) produceExtraReIPInstructions(options *VReIPOptions, oldVdb *VCoordinationDatabase) (
	[]clusterOp, *VCoordinationDatabase, error) {
	var instructions []clusterOp

	// build a new vdb with the new IPs in the re-ip list
	newVdb := options.genNewVdb(oldVdb, vcc.Log)
	oldHosts := maps.Keys(oldVdb.HostNodeMap)
	if len(oldHosts) != len(newVdb.HostList) {
		return instructions, newVdb, fmt.Errorf("the number of new hosts does not match the number of nodes in original database")
	}

	nmaPrepareDirectoriesOp, err := makeNMAPrepareDirsUseExistingDirOp(newVdb.HostNodeMap, true, /*force cleanup*/
		true /*for db revive*/, true /*use existing dir*/, false /*useExistingDepotDirOnly*/)
	if err != nil {
		return instructions, newVdb, err
	}

	nmaNetworkProfilePostReip := makeNMANetworkProfileOp(newVdb.HostList)
	nmaLoadRemoteCatalogOp := makeNMALoadRemoteCatalogForInPlaceRevive(oldHosts, options.ConfigurationParameters,
		newVdb, options.SandboxName)

	// confirm at least one host has the latest catalog
	nmaReadCatEdOp, err := makeNMAReadCatalogEditorOpForInPlaceRevive(newVdb, options.SandboxName, options.newAddresses)
	if err != nil {
		return instructions, newVdb, err
	}
	instructions = append(instructions, &nmaPrepareDirectoriesOp, &nmaNetworkProfilePostReip, &nmaLoadRemoteCatalogOp, &nmaReadCatEdOp)

	return instructions, newVdb, nil
}

func (options *VReIPOptions) genNewVdb(vdb *VCoordinationDatabase, logger vlog.Printer) *VCoordinationDatabase {
	// create a node name to host map
	nodenameToHostMap := make(map[string]string)
	for host, vnode := range vdb.HostNodeMap {
		nodenameToHostMap[vnode.Name] = host
	}
	newVdb := new(VCoordinationDatabase)
	newVdb.HostNodeMap = util.CopyMap(vdb.HostNodeMap)
	newVdb.Name = vdb.Name
	newVdb.CommunalStorageLocation = vdb.CommunalStorageLocation

	// replace old IPs with new IPs
	for i, info := range options.ReIPList {
		if host, exists := nodenameToHostMap[info.NodeName]; exists {
			// in case the re-ip item is given as: node name -> new IP
			vnode := newVdb.HostNodeMap[host]
			vnode.Address = info.TargetAddress
			newVdb.HostNodeMap[info.TargetAddress] = vnode
			delete(newVdb.HostNodeMap, host)
		} else if vnode, exists := vdb.HostNodeMap[info.NodeAddress]; exists {
			// in case the re-ip item is given as: original IP -> new IP
			originalAddress := info.NodeAddress
			vnode.Address = info.TargetAddress
			newVdb.HostNodeMap[info.TargetAddress] = vnode
			delete(newVdb.HostNodeMap, originalAddress)
			// add node name info to this item in the re-ip list
			info.NodeName = vnode.Name
			options.ReIPList[i] = info
		} else {
			logger.PrintWarning("Node name %q or address %q not found in vdb, ignoring this re-ip item for further processing",
				info.NodeName, info.NodeAddress)
		}
	}

	newVdb.HostList = maps.Keys(newVdb.HostNodeMap)
	sort.Strings(newVdb.HostList)

	return newVdb
}

type reIPRow struct {
	CurrentAddress      string `json:"from_address"`
	NewAddress          string `json:"to_address"`
	NewControlAddress   string `json:"to_control_address,omitempty"`
	NewControlBroadcast string `json:"to_control_broadcast,omitempty"`
}

// ReadReIPFile reads the re-IP file and builds a slice of ReIPInfo.
// It returns any error encountered.
func (options *VReIPOptions) ReadReIPFile(path string) error {
	if err := util.AbsPathCheck(path); err != nil {
		return fmt.Errorf("must specify an absolute path for the re-ip file")
	}

	var reIPRows []reIPRow
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("fail to read the re-ip file %s, details: %w", path, err)
	}
	err = json.Unmarshal(fileBytes, &reIPRows)
	if err != nil {
		return fmt.Errorf("fail to unmarshal the re-ip file, details: %w", err)
	}

	addressCheck := func(address string, ipv6 bool) error {
		checkPassed := false
		if ipv6 {
			checkPassed = util.IsIPv6(address)
		} else {
			checkPassed = util.IsIPv4(address)
		}

		if !checkPassed {
			ipVersion := "IPv4"
			if ipv6 {
				ipVersion = "IPv6"
			}
			return fmt.Errorf("%s in the re-ip file is not a valid %s address", address, ipVersion)
		}

		return nil
	}

	ipv6 := options.IPv6
	for _, row := range reIPRows {
		var info ReIPInfo
		info.NodeAddress = row.CurrentAddress
		if e := addressCheck(row.CurrentAddress, ipv6); e != nil {
			return e
		}

		info.TargetAddress = row.NewAddress
		info.TargetControlAddress = row.NewControlAddress
		info.TargetControlBroadcast = row.NewControlBroadcast

		options.ReIPList = append(options.ReIPList, info)
	}

	return nil
}
