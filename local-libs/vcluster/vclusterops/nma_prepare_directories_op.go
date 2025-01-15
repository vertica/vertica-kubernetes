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

	"golang.org/x/exp/maps"
)

type nmaPrepareDirectoriesOp struct {
	opBase
	hostRequestBodyMap map[string]string
	forceCleanup       bool
	forRevive          bool
}

type prepareDirectoriesRequestData struct {
	CatalogPath          string   `json:"catalog_path"`
	DepotPath            string   `json:"depot_path,omitempty"`
	StorageLocations     []string `json:"storage_locations,omitempty"`
	UserStorageLocations []string `json:"user_storage_locations,omitempty"`
	ForceCleanup         bool     `json:"force_cleanup"`
	ForRevive            bool     `json:"for_revive"`
	IgnoreParent         bool     `json:"ignore_parent"`
}

func makeNMAPrepareDirectoriesOp(hostNodeMap vHostNodeMap,
	forceCleanup, forRevive bool) (nmaPrepareDirectoriesOp, error) {
	op := nmaPrepareDirectoriesOp{}
	op.name = "NMAPrepareDirectoriesOp"
	op.description = "Create necessary directories on Vertica hosts"
	op.forceCleanup = forceCleanup
	op.forRevive = forRevive

	err := op.setupRequestBody(hostNodeMap)
	if err != nil {
		return op, err
	}

	op.hosts = maps.Keys(hostNodeMap)

	return op, nil
}

func (op *nmaPrepareDirectoriesOp) setupRequestBody(hostNodeMap vHostNodeMap) error {
	op.hostRequestBodyMap = make(map[string]string)

	for host := range hostNodeMap {
		prepareDirData := prepareDirectoriesRequestData{}
		prepareDirData.CatalogPath = getCatalogPath(hostNodeMap[host].CatalogPath)
		prepareDirData.DepotPath = hostNodeMap[host].DepotPath
		prepareDirData.StorageLocations = hostNodeMap[host].StorageLocations
		prepareDirData.UserStorageLocations = hostNodeMap[host].UserStorageLocations
		prepareDirData.ForceCleanup = op.forceCleanup
		prepareDirData.ForRevive = op.forRevive
		prepareDirData.IgnoreParent = false

		dataBytes, err := json.Marshal(prepareDirData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}
	op.logger.Info("request data", "op name", op.name, "hostRequestBodyMap", op.hostRequestBodyMap)

	return nil
}

func (op *nmaPrepareDirectoriesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("directories/prepare")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaPrepareDirectoriesOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaPrepareDirectoriesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaPrepareDirectoriesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaPrepareDirectoriesOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			// the response_obj will be a dictionary like the following:
			// {'/data/good/procedures': 'created',
			//  '/data/good/v_good_node0002_catalog': 'created',
			//  '/data/good/v_good_node0003_data': 'created',
			//  '/data/good/v_good_node0003_depot': 'created',
			//  '/opt/vertica/config/logrotate': 'created'}
			_, err := op.parseAndCheckMapResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
