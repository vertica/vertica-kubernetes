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
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

const (
	spreadConf  = "config/spread"
	verticaConf = "config/vertica"
)

type nmaDownloadConfigOp struct {
	opBase
	catalogPathMap map[string]string
	endpoint       string
	fileContent    *string
	vdb            *VCoordinationDatabase
	sandbox        *string
}

func makeNMADownloadConfigOp(
	opName string,
	sourceConfigHost []string,
	endpoint string,
	fileContent *string,
	vdb *VCoordinationDatabase,
	sandbox *string,
) nmaDownloadConfigOp {
	op := nmaDownloadConfigOp{}
	op.name = opName
	op.hosts = sourceConfigHost
	op.endpoint = endpoint
	if op.endpoint == verticaConf {
		op.description = "Get contents of vertica.conf"
	} else if op.endpoint == spreadConf {
		op.description = "Get contents of spread.conf"
	}
	op.fileContent = fileContent
	op.vdb = vdb
	op.sandbox = sandbox
	return op
}

func (op *nmaDownloadConfigOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint(op.endpoint)

		catalogPath, ok := op.catalogPathMap[host]
		if !ok {
			return fmt.Errorf("[%s] fail to get catalog path from host %s", op.name, host)
		}
		httpRequest.QueryParams = map[string]string{"catalog_path": catalogPath}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaDownloadConfigOp) prepare(execContext *opEngineExecContext) error {
	op.catalogPathMap = make(map[string]string)
	// vdb is built by calling /cluster and /nodes endpoints of a running db.
	// If nodes' info is not available in vdb, we will get the host from execContext.nmaVDatabase which is build by reading the catalog editor
	if op.vdb == nil || len(op.vdb.HostNodeMap) == 0 {
		nmaVDB := execContext.nmaVDatabase
		if op.hosts == nil {
			var hostsWithLatestCatalog []string
			// If SpreadEncryption is enabled, the primary node with the latest catalog will be the sourceConfigHost.
			if nmaVDB.SpreadEncryption != "" {
				hostsWithLatestCatalog = getPrimaryHostsWithLatestCatalog(&nmaVDB, execContext.hostsWithLatestCatalog, execContext)
				if len(hostsWithLatestCatalog) == 0 {
					return fmt.Errorf("could not find at least one primary host with the latest catalog")
				}
			} else {
				// If the host input is a nil value, we find the host with the latest catalog version to update the host input.
				// Otherwise, we use the host input.
				hostsWithLatestCatalog = execContext.hostsWithLatestCatalog
				if len(hostsWithLatestCatalog) == 0 {
					return fmt.Errorf("could not find at least one host with the latest catalog")
				}
			}
			hostWithLatestCatalog := hostsWithLatestCatalog[:1]
			// update the host with the latest catalog
			op.hosts = hostWithLatestCatalog
		}
		// For createDb and AddNodes, sourceConfigHost input is the bootstrap host.
		// we update the catalogPathMap for next download operation's steps from information of catalog editor
		err := updateCatalogPathMapFromCatalogEditor(op.hosts, &nmaVDB, op.catalogPathMap)
		if err != nil {
			return fmt.Errorf("failed to get catalog paths from catalog editor: %w", err)
		}
		// If vdb contains nodes' info, we will check if there are any primary up nodes.
		// If we found any primary up nodes, we set catalogPathMap based on their info in vdb.
	} else {
		// This case is used for starting nodes operation.
		// Otherwise, we set catalogPathMap from the catalog editor (start_db, create_db).
		// For startNodes, If the sourceConfigHost input is a nil value, we find any UP primary nodes as source host to update the host input.
		// we update the catalogPathMap for next download operation's steps from node information by using HTTPS /v1/nodes
		var primaryUpHosts []string
		var upHosts []string
		for host, vnode := range op.vdb.HostNodeMap {
			if vnode.State == util.NodeUpState {
				// If we do not find a primary up host in the same cluster(or sandbox), try to find a secondary up host
				if vnode.IsPrimary {
					primaryUpHosts = append(primaryUpHosts, host)
					op.catalogPathMap[host] = getCatalogPath(vnode.CatalogPath)
					break
				} else if op.sandbox != nil && vnode.Sandbox == *op.sandbox {
					upHosts = append(upHosts, host)
					op.catalogPathMap[host] = getCatalogPath(vnode.CatalogPath)
				}
			}
		}
		if len(primaryUpHosts) == 0 {
			op.logger.Info("could not find any primary UP nodes, considering secondary UP nodes.")
			if len(upHosts) == 0 {
				return fmt.Errorf("could not find any up nodes")
			}
			op.hosts = []string{upHosts[0]}
		} else {
			op.hosts = primaryUpHosts
		}
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaDownloadConfigOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaDownloadConfigOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaDownloadConfigOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		// VER-88362 will re-enable the result details and hide sensitive info in it
		op.logger.Info("Download config file result",
			"op name", op.name, "host", host, "status", result.status.getStatusString())
		if result.isPassing() {
			// The content of config file will be stored as content of the response
			*op.fileContent = result.content
			return nil
		}
		allErrs = errors.Join(allErrs, result.err)
	}

	return appendHTTPSFailureError(allErrs)
}
