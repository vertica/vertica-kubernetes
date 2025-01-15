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
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

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
}

func VReIPFactory() VReIPOptions {
	options := VReIPOptions{}
	// set default values to the params
	options.setDefaultValues()
	options.TrimReIPList = false
	options.SandboxName = util.MainClusterSandbox
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
		vcc.LogInfo("Re-IP the sandbox %s", options.SandboxName)
		runError := clusterOpEngine.runInSandbox(vcc.Log, pVDB, options.SandboxName)
		if runError != nil {
			return fmt.Errorf("fail to re-ip: %w", runError)
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
			options.usePassword, options.UserName, sandbox, mainCluster, options.Password, ReIP)
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
	nmaNetworkProfileOp := makeNMANetworkProfileOp(newAddresses)

	instructions = append(instructions, &nmaNetworkProfileOp)

	vdbWithPrimaryNodes := new(VCoordinationDatabase)
	// When we cannot get db info from cluster_config.json, we will fetch it from NMA /nodes endpoint.
	if vdb == nil {
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
		// use a copy of vdb because we want to keep secondary nodes in vdb for next nmaReIPOP
		*vdbWithPrimaryNodes = *vdb
		vdbWithPrimaryNodes.filterPrimaryNodes()
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
	nmaReIPOP := makeNMAReIPOp(options.ReIPList, vdb, options.TrimReIPList)

	instructions = append(instructions, &nmaReIPOP)

	return instructions, nil
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
