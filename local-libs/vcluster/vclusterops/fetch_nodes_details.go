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

type NodeState struct {
	Name                     string   `json:"name"`
	ID                       uint64   `json:"node_id"`
	Address                  string   `json:"address"`
	State                    string   `json:"state"`
	Database                 string   `json:"database"`
	IsPrimary                bool     `json:"is_primary"`
	IsReadOnly               bool     `json:"is_readonly"`
	CatalogPath              string   `json:"catalog_path"`
	DataPath                 []string `json:"data_path"`
	DepotPath                string   `json:"depot_path"`
	SubclusterName           string   `json:"subcluster_name"`
	SubclusterID             uint64   `json:"subcluster_id"`
	LastMsgFromNodeAt        string   `json:"last_msg_from_node_at"`
	DownSince                string   `json:"down_since"`
	Version                  string   `json:"build_info"`
	SandboxName              string   `json:"sandbox_name"`
	NumberShardSubscriptions uint     `json:"number_shard_subscriptions"`
}

type StorageLocation struct {
	Name        string `json:"name"`
	ID          uint64 `json:"location_id"`
	Label       string `json:"label"`
	UsageType   string `json:"location_usage_type"`
	Path        string `json:"location_path"`
	SharingType string `json:"location_sharing_type"`
	MaxSize     uint64 `json:"max_size"`
	DiskPercent string `json:"disk_percent"`
	HasCatalog  bool   `json:"has_catalog"`
	Retired     bool   `json:"retired"`
}

type StorageLocations struct {
	StorageLocList []StorageLocation `json:"storage_location_list"`
}

type NodeDetails struct {
	NodeState
	StorageLocations
}

type NodesDetails []NodeDetails

type hostNodeDetailsMap map[string]*NodeDetails

type VFetchNodesDetailsOptions struct {
	DatabaseOptions
}

func VFetchNodesDetailsOptionsFactory() VFetchNodesDetailsOptions {
	options := VFetchNodesDetailsOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VFetchNodesDetailsOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VFetchNodesDetailsOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(FetchNodesDetailsCmd, logger)
	if err != nil {
		return err
	}

	return nil
}

func (options *VFetchNodesDetailsOptions) analyzeOptions() (err error) {
	// resolve RawHosts to be IP addresses
	if len(options.RawHosts) > 0 {
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VFetchNodesDetailsOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VFetchNodesDetails can return nodes' details including node state and storage locations for the provided hosts
func (vcc VClusterCommands) VFetchNodesDetails(options *VFetchNodesDetailsOptions) (nodesDetails NodesDetails, err error) {
	/*
	 *   - Validate Options
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return nodesDetails, err
	}

	hostsWithNodeDetails := make(hostNodeDetailsMap, len(options.Hosts))

	instructions, err := vcc.produceFetchNodesDetailsInstructions(options, hostsWithNodeDetails)
	if err != nil {
		return nodesDetails, fmt.Errorf("fail to produce instructions: %w", err)
	}

	clusterOpEngine := makeClusterOpEngine(instructions, options)

	err = clusterOpEngine.run(vcc.Log)
	if err != nil {
		return nodesDetails, fmt.Errorf("failed to fetch node details on hosts %v: %w", options.Hosts, err)
	}

	for _, nodeDetails := range hostsWithNodeDetails {
		nodesDetails = append(nodesDetails, *nodeDetails)
	}

	return nodesDetails, nil
}

// produceFetchNodesDetails will build a list of instructions to execute for
// the fetch node details operation.
//
// The generated instructions will later perform the following operations:
//   - Get nodes' state by calling /v1/node
//   - Get nodes' storage locations by calling /v1/node/storage-locations
func (vcc *VClusterCommands) produceFetchNodesDetailsInstructions(options *VFetchNodesDetailsOptions,
	hostsWithNodeDetails hostNodeDetailsMap) ([]clusterOp, error) {
	var instructions []clusterOp

	// when password is specified, we will use username/password to call https endpoints
	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}

	httpsGetNodeStateOp, err := makeHTTPSGetLocalNodeStateOp(options.DBName, options.Hosts,
		options.usePassword, options.UserName, options.Password, hostsWithNodeDetails)
	if err != nil {
		return instructions, err
	}

	httpsGetStorageLocationsOp, err := makeHTTPSGetStorageLocsOp(options.Hosts, options.usePassword,
		options.UserName, options.Password, hostsWithNodeDetails)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&httpsGetNodeStateOp,
		&httpsGetStorageLocationsOp,
	)

	return instructions, nil
}
