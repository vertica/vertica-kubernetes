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
	"strings"

	"github.com/vertica/vcluster/rfc7807"
	"golang.org/x/exp/maps"
)

type nmaPrepareDirectoriesOp struct {
	opBase
	hostRequestBodyMap map[string]string
	forceCleanup       bool
	forRevive          bool
	// Hidden option for internal use
	// Used in db revive - re-use existing catalog directories for reviving if true
	useExistingCatalogDir bool
	// used for add_subcluster to reuse existing depot dirs
	useExistingDepotDirOnly bool
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
	op.useExistingCatalogDir = false
	op.useExistingDepotDirOnly = false

	err := op.setupRequestBody(hostNodeMap)
	if err != nil {
		return op, err
	}

	op.hosts = maps.Keys(hostNodeMap)

	return op, nil
}

func makeNMAPrepareDirsUseExistingDirOp(hostNodeMap vHostNodeMap,
	forceCleanup, forRevive bool, useExistingCatalogDir, useExistingDepotDir bool) (nmaPrepareDirectoriesOp, error) {
	op, err := makeNMAPrepareDirectoriesOp(hostNodeMap, forceCleanup, forRevive)
	if err != nil {
		return op, err
	}
	op.useExistingCatalogDir = useExistingCatalogDir
	op.useExistingDepotDirOnly = useExistingDepotDir
	if op.useExistingDirs() {
		op.name = "NMAPrepareDirsAllowUsingExistingDirOp"
		op.description = "Create necessary directories on Vertica hosts, allowing using existing directories: "

		existingDirList := []string{}
		if op.useExistingCatalogDir {
			existingDirList = append(existingDirList, "all non /Catalog dirs")
		}
		if op.useExistingDepotDirOnly {
			existingDirList = append(existingDirList, "depot dir")
		}
		op.description += strings.Join(existingDirList, ",")
	}
	return op, nil
}

func (op *nmaPrepareDirectoriesOp) useExistingDirs() bool {
	return op.useExistingCatalogDir || op.useExistingDepotDirOnly
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
		// this option is hidden from user interface
		// we have to ignore parent in this case for re-using depot dir
		// because otherwise the NMA will error out
		if op.useExistingDepotDirOnly {
			prepareDirData.IgnoreParent = true
		}

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
			// if catalog directory exists and user specified using existing dir, skip the error
			if op.useExistingDirs() {
				rfcError := &rfc7807.VProblem{}

				if op.useExistingCatalogDir {
					if isRFCError := errors.As(result.err, &rfcError); isRFCError && (rfcError.ProblemID == rfc7807.CreateDirectoryExistError) {
						op.logger.Info("using existing catalog directory", "details", result.err.Error())
						continue
					}
				}
				if op.useExistingDepotDirOnly {
					isRFCError := errors.As(result.err, &rfcError)
					if isRFCError && (rfcError.ProblemID == rfc7807.CreateDirectoryExistError) {
						op.logger.Info("using existing depot directory", "details", result.err.Error())
						continue
					} else if isRFCError && (rfcError.ProblemID == rfc7807.CreateDirectoryParentDirectoryExists) {
						op.logger.Info("using existing depot parent directory", "details", result.err.Error())
						continue
					}
				}
			}

			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
