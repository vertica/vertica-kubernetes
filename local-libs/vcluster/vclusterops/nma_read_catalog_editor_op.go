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

	"github.com/vertica/vcluster/rfc7807"
	"github.com/vertica/vcluster/vclusterops/util"
	"golang.org/x/exp/maps"
)

type nmaReadCatalogEditorOp struct {
	opBase
	initiator      []string // used when creating new nodes
	vdb            *VCoordinationDatabase
	catalogPathMap map[string]string

	firstStartAfterRevive bool // used for start_db only

	// for passing state between execute() and finalize() and
	// should not be initialized by the factory function
	allErrs                error
	hostsWithLatestCatalog []string
	latestNmaVDB           nmaVDatabase
	bestHost               string
	sandbox                string
}

// makeNMAReadCatalogEditorOpWithInitiator creates an op to read catalog editor info.
// Initiator is needed when creating new nodes
func makeNMAReadCatalogEditorOpWithInitiator(
	initiator []string,
	vdb *VCoordinationDatabase,
) (nmaReadCatalogEditorOp, error) {
	op := nmaReadCatalogEditorOp{}
	op.name = "NMAReadCatalogEditorOp"
	op.description = "Read catalog"
	op.initiator = initiator
	op.vdb = vdb
	return op, nil
}

// under sandbox mode, each sandbox are using different files for vcluster catalog,
// when accessing catalog editor for a sandbox, we need to feed sandbox as the op's parameter
func makeNMAReadCatalogEditorOpWithSandbox(vdb *VCoordinationDatabase, sandbox string) (nmaReadCatalogEditorOp, error) {
	op, err := makeNMAReadCatalogEditorOpWithInitiator([]string{}, vdb)
	op.sandbox = sandbox
	return op, err
}

// makeNMAReadCatalogEditorOp creates an op to read catalog editor info.
func makeNMAReadCatalogEditorOp(vdb *VCoordinationDatabase) (nmaReadCatalogEditorOp, error) {
	return makeNMAReadCatalogEditorOpWithInitiator([]string{}, vdb)
}

func makeNMAReadCatalogEditorOpForStartDB(
	vdb *VCoordinationDatabase,
	firstStartAfterRevive bool,
	sandbox string) (nmaReadCatalogEditorOp, error) {
	op, err := makeNMAReadCatalogEditorOpWithInitiator([]string{}, vdb)
	if err != nil {
		return op, err
	}
	op.sandbox = sandbox
	op.firstStartAfterRevive = firstStartAfterRevive
	return op, err
}

func (op *nmaReadCatalogEditorOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("catalog/database")

		catalogPath, ok := op.catalogPathMap[host]
		if !ok {
			err := fmt.Errorf("[%s] cannot find catalog path of host %s", op.name, host)
			op.logger.Error(err, "fail to find catalog path, detail")
			return err
		}
		httpRequest.QueryParams = map[string]string{"catalog_path": catalogPath}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaReadCatalogEditorOp) prepare(execContext *opEngineExecContext) error {
	// build a map from host to catalog path
	// if the initiator host(s) are given, only build map for these hosts
	op.catalogPathMap = make(map[string]string)
	if len(op.initiator) == 0 {
		op.logger.Info("Using all hosts in host to node map", "op name", op.name)
		op.hosts = maps.Keys(op.vdb.HostNodeMap)
		for host, vnode := range op.vdb.HostNodeMap {
			op.catalogPathMap[host] = vnode.CatalogPath
		}
	} else {
		op.logger.Info("Using initiator hosts only", "op name", op.name)
		for _, host := range op.initiator {
			op.hosts = append(op.hosts, host)
			vnode, ok := op.vdb.HostNodeMap[host]
			if !ok {
				return fmt.Errorf("[%s] cannot find the initiator host %s from vdb.HostNodeMap %+v",
					op.name, host, op.vdb.HostNodeMap)
			}
			op.catalogPathMap[host] = vnode.CatalogPath
		}
	}

	if len(op.hosts) == 0 {
		op.skipExecute = true
		op.logger.Info("No hosts found, skipping execution", "op name", op.name)
		return nil
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaReadCatalogEditorOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

type nmaVersions struct {
	Global      json.Number `json:"global"`
	Local       json.Number `json:"local"`
	Session     json.Number `json:"session"`
	Spread      json.Number `json:"spread"`
	Transaction json.Number `json:"transaction"`
	TwoPhaseID  json.Number `json:"two_phase_id"`
}

type nmaVNode struct {
	Address              string      `json:"address"`
	AddressFamily        string      `json:"address_family"`
	CatalogPath          string      `json:"catalog_path"`
	ClientPort           json.Number `json:"client_port"`
	ControlAddress       string      `json:"control_address"`
	ControlAddressFamily string      `json:"control_address_family"`
	ControlBroadcast     string      `json:"control_broadcast"`
	ControlNode          json.Number `json:"control_node"`
	ControlPort          json.Number `json:"control_port"`
	EiAddress            json.Number `json:"ei_address"`
	HasCatalog           bool        `json:"has_catalog"`
	IsEphemeral          bool        `json:"is_ephemeral"`
	IsPrimary            bool        `json:"is_primary"`
	IsRecoveryClerk      bool        `json:"is_recovery_clerk"`
	Name                 string      `json:"name"`
	NodeParamMap         []any       `json:"node_param_map"`
	NodeType             json.Number `json:"node_type"`
	Oid                  json.Number `json:"oid"`
	ParentFaultGroupID   json.Number `json:"parent_fault_group_id"`
	ReplacedNode         json.Number `json:"replaced_node"`
	Schema               json.Number `json:"schema"`
	SiteUniqueID         json.Number `json:"site_unique_id"`
	StartCommand         []string    `json:"start_command"`
	StorageLocations     []string    `json:"storage_locations"`
	Tag                  json.Number `json:"tag"`
	Subcluster           struct {
		Name        string `json:"sc_name"`
		IsPrimary   bool   `json:"is_primary_sc"`
		IsDefault   bool   `json:"is_default"`
		IsSandbox   bool   `json:"sandbox"`
		SandboxName string `json:"sandbox_name"`
	} `json:"sc_details"`
}

type nmaVDatabase struct {
	Name     string      `json:"name"`
	Versions nmaVersions `json:"versions"`
	Nodes    []nmaVNode  `json:"nodes"`
	// this map will not be unmarshaled but will be used in NMAStartNodeOp
	HostNodeMap             map[string]*nmaVNode `json:",omitempty"`
	ControlMode             string               `json:"control_mode"`
	WillUpgrade             bool                 `json:"will_upgrade"`
	SpreadEncryption        string               `json:"spread_encryption"`
	CommunalStorageLocation string               `json:"communal_storage_location"`
	// primary node count will not be unmarshaled but will be used in NMAReIPOp
	PrimaryNodeCount uint `json:",omitempty"`
}

func (op *nmaReadCatalogEditorOp) processResult(_ *opEngineExecContext) error {
	var maxGlobalVersion int64
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			nmaVDB := nmaVDatabase{}
			err := op.parseAndCheckResponse(host, result.content, &nmaVDB)
			if err != nil {
				err = fmt.Errorf("[%s] fail to parse result on host %s, details: %w",
					op.name, host, err)
				op.allErrs = errors.Join(op.allErrs, err)
				continue
			}

			var primaryNodeCount uint
			// build host to node map for NMAStartNodeOp
			hostNodeMap := make(map[string]*nmaVNode)
			for i := 0; i < len(nmaVDB.Nodes); i++ {
				n := nmaVDB.Nodes[i]
				hostNodeMap[n.Address] = &n
				if n.IsPrimary {
					primaryNodeCount++
				}
			}
			nmaVDB.HostNodeMap = hostNodeMap
			nmaVDB.PrimaryNodeCount = primaryNodeCount

			// find hosts with latest catalog version
			globalVersion, err := nmaVDB.Versions.Global.Int64()
			if err != nil {
				err = fmt.Errorf("[%s] fail to convert spread Version to integer %s, details: %w",
					op.name, host, err)
				op.allErrs = errors.Join(op.allErrs, err)
				continue
			}
			if globalVersion > maxGlobalVersion {
				op.hostsWithLatestCatalog = []string{host}
				maxGlobalVersion = globalVersion
				// save the latest NMAVDatabase to execContext
				op.latestNmaVDB = nmaVDB
				op.bestHost = host
			} else if globalVersion == maxGlobalVersion {
				op.hostsWithLatestCatalog = append(op.hostsWithLatestCatalog, host)
			}
		} else {
			// if this is not the first time of start_db after revive_db,
			// we ignore the error if the catalog directory is empty, because
			// - we may send request to a secondary node right after revive
			// - users may delete the catalog files
			if !op.firstStartAfterRevive {
				rfcError := &rfc7807.VProblem{}
				if ok := errors.As(result.err, &rfcError); ok &&
					(rfcError.ProblemID == rfc7807.CECatalogContentDirEmptyError ||
						rfcError.ProblemID == rfc7807.CECatalogContentDirNotExistError) {
					continue
				}
			}

			op.allErrs = errors.Join(op.allErrs, result.err)
		}
	}

	// let finalize() handle error conditions, in case this function is skipped
	return nil
}

// finalize contains the final logic that would otherwise be in execute, but since execute
// shouldn't be called with no hosts in the host list, doing the work here allows handling
// the errors whether or not execute was called.
func (op *nmaReadCatalogEditorOp) finalize(execContext *opEngineExecContext) error {
	// save hostsWithLatestCatalog to execContext
	if len(op.hostsWithLatestCatalog) == 0 {
		err := fmt.Errorf("[%s] cannot find any host with the latest catalog", op.name)
		op.allErrs = errors.Join(op.allErrs, err)
		return op.allErrs
	}

	execContext.hostsWithLatestCatalog = op.hostsWithLatestCatalog
	// save the latest nmaVDB to execContext
	execContext.nmaVDatabase = op.latestNmaVDB
	op.logger.PrintInfo("reporting results as obtained from the host [%s] ", op.bestHost)
	// when starting sandboxes, we just need one passing result from a primary node
	// and we return successfully once we have it.
	if op.sandbox != util.MainClusterSandbox {
		return nil
	}
	return op.allErrs
}
