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
)

type nmaReIPOp struct {
	opBase
	reIPList             []ReIPInfo
	vdb                  *VCoordinationDatabase
	primaryNodeCount     uint
	hostRequestBodyMap   map[string]string
	mapHostToNodeName    map[string]string
	mapHostToCatalogPath map[string]string
	trimReIPData         bool
}

func makeNMAReIPOp(
	reIPList []ReIPInfo,
	vdb *VCoordinationDatabase,
	trimReIPData bool) nmaReIPOp {
	op := nmaReIPOp{}
	op.name = "NMAReIPOp"
	op.description = "Update node IPs in catalog"
	op.reIPList = reIPList
	op.vdb = vdb
	op.trimReIPData = trimReIPData
	return op
}

type ReIPInfo struct {
	NodeName               string `json:"node_name"`
	NodeAddress            string `json:"-"`
	TargetAddress          string `json:"address"`
	TargetControlAddress   string `json:"control_address"`
	TargetControlBroadcast string `json:"control_broadcast"`
}

type reIPParams struct {
	CatalogPath  string     `json:"catalog_path"`
	ReIPInfoList []ReIPInfo `json:"re_ip_list"`
}

func (op *nmaReIPOp) updateRequestBody(_ *opEngineExecContext) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		var p reIPParams
		p.CatalogPath = op.mapHostToCatalogPath[host]
		p.ReIPInfoList = op.reIPList
		dataBytes, err := json.Marshal(p)
		if err != nil {
			op.logger.Error(err, `[%s] fail to marshal request data to JSON string, detail %s`, op.name)
			return err
		}
		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	op.logger.Info("request data", "op name", op.name, "hostRequestBodyMap", op.hostRequestBodyMap)
	return nil
}

func (op *nmaReIPOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PutMethod
		httpRequest.buildNMAEndpoint("catalog/re-ip")
		httpRequest.RequestData = op.hostRequestBodyMap[host]

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

// updateReIPList is used for the vcluster CLI to update node names
func (op *nmaReIPOp) updateReIPList(execContext *opEngineExecContext) error {
	hostNodeMap := execContext.nmaVDatabase.HostNodeMap

	for i := 0; i < len(op.reIPList); i++ {
		info := op.reIPList[i]
		// update node name if not given
		if info.NodeName == "" {
			vnode, ok := hostNodeMap[info.NodeAddress]
			if !ok {
				return fmt.Errorf("the provided IP %s cannot be found from the database catalog",
					info.NodeAddress)
			}
			info.NodeName = vnode.Name
		}
		// update control address if not given
		if info.TargetControlAddress == "" {
			info.TargetControlAddress = info.TargetAddress
		}
		// update control broadcast if not given
		if info.TargetControlBroadcast == "" {
			profile, ok := execContext.networkProfiles[info.TargetAddress]
			if !ok {
				return fmt.Errorf("[%s] unable to find network profile for address %s", op.name, info.TargetAddress)
			}
			info.TargetControlBroadcast = profile.Broadcast
		}

		op.reIPList[i] = info
	}

	return nil
}

// trimReIPList removes nodes, based on catalog editor info,
// which are not among the nodes with latest catalog
func (op *nmaReIPOp) trimReIPList(execContext *opEngineExecContext) error {
	nodeNamesWithLatestCatalog := make(map[string]struct{})
	for i := range execContext.nmaVDatabase.Nodes {
		vnode := execContext.nmaVDatabase.Nodes[i]
		nodeNamesWithLatestCatalog[vnode.Name] = struct{}{}
	}

	var trimmedReIPList []ReIPInfo
	nodesToTrim := make(map[string]string)
	for _, reIPInfo := range op.reIPList {
		if _, exist := nodeNamesWithLatestCatalog[reIPInfo.NodeName]; exist {
			trimmedReIPList = append(trimmedReIPList, reIPInfo)
		} else {
			nodesToTrim[reIPInfo.NodeName] = reIPInfo.NodeAddress
		}
	}

	if len(nodesToTrim) > 0 {
		// throw an error if not automatically trim the re-ip list
		if !op.trimReIPData {
			return fmt.Errorf("[%s] the following nodes from the re-ip list do not exist in the catalog: %+v",
				op.name, nodesToTrim)
		}

		// otherwise, trim the re-ip list
		op.logger.Info("re-ip list is trimmed", "trimmed re-ip list", trimmedReIPList)
	}

	op.reIPList = trimmedReIPList
	return nil
}

// whetherSkipReIP decides whether skip calling the re-ip endpoint; skip it in case that
// the target addresses in the re-ip list match the node addresses in catalog.
// Return true if skip.
func (op *nmaReIPOp) whetherSkipReIP(execContext *opEngineExecContext) bool {
	// node name to address map retrieved from catalog
	nodeAddressMap := make(map[string]string)
	for h, n := range execContext.nmaVDatabase.HostNodeMap {
		nodeAddressMap[n.Name] = h
	}

	// we should run re-ip if any node's target address is different from its existing one
	for _, reIPInfo := range op.reIPList {
		nodeAddress, exist := nodeAddressMap[reIPInfo.NodeName]
		if !exist {
			return false
		}
		if reIPInfo.TargetAddress != nodeAddress {
			return false
		}
	}

	op.logger.PrintInfo("[%s] all target addresses already exist in the catalog, no need to re-ip.",
		op.name)
	return true
}

func (op *nmaReIPOp) prepare(execContext *opEngineExecContext) error {
	// build mapHostToNodeName and catalogPathMap from vdb
	op.mapHostToNodeName = make(map[string]string)
	op.mapHostToCatalogPath = make(map[string]string)
	for host, vnode := range op.vdb.HostNodeMap {
		op.mapHostToNodeName[host] = vnode.Name
		op.mapHostToCatalogPath[host] = vnode.CatalogPath
	}

	// get the primary node names
	// this step is needed as the new host addresses
	// are not in the catalog
	primaryNodes := make(map[string]struct{})
	nodeList := execContext.nmaVDatabase.Nodes
	for i := 0; i < len(nodeList); i++ {
		vnode := nodeList[i]
		if vnode.IsPrimary {
			primaryNodes[vnode.Name] = struct{}{}
		}
	}

	// update the hosts
	for _, host := range execContext.hostsWithLatestCatalog {
		nodeName := op.mapHostToNodeName[host]
		if _, ok := primaryNodes[nodeName]; ok {
			op.hosts = append(op.hosts, host)
		}
	}

	// get primary node count
	op.primaryNodeCount = execContext.nmaVDatabase.PrimaryNodeCount

	// quorum check
	if !op.hasQuorum(uint(len(op.hosts)), op.primaryNodeCount) {
		return fmt.Errorf("failed quorum check, not enough primaries exist with: %d", len(op.hosts))
	}

	// update re-ip list
	err := op.updateReIPList(execContext)
	if err != nil {
		return fmt.Errorf("[%s] error updating reIP list: %w", op.name, err)
	}

	// trim re-ip list for clients such as K8s
	err = op.trimReIPList(execContext)
	if err != nil {
		return err
	}

	// if no new IP provided in the re-ip list
	// we will skip calling the re-ip endpoint
	if op.whetherSkipReIP(execContext) {
		op.skipExecute = true
		return nil
	}

	// build request body for hosts
	err = op.updateRequestBody(execContext)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaReIPOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaReIPOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaReIPOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	var successCount uint
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			var reIPResult []ReIPInfo
			err := op.parseAndCheckResponse(host, result.content, &reIPResult)
			if err != nil {
				err = fmt.Errorf("[%s] fail to parse result on host %s, details: %w",
					op.name, host, err)
				allErrs = errors.Join(allErrs, err)
				continue
			}

			successCount++
		} else {
			allErrs = errors.Join(allErrs, result.err)
			// VER-88054 rollback the commits
		}
	}

	// quorum check
	if !op.hasQuorum(successCount, op.primaryNodeCount) {
		// VER-88054 rollback the commits
		err := fmt.Errorf("failed quroum check for re-ip update. Success count: %d", successCount)
		allErrs = errors.Join(allErrs, err)
	}

	return allErrs
}
