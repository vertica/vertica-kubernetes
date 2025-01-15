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
	"path/filepath"
	"strings"
	"time"
)

type leaseCheckOption int

const (
	leaseCheckOnly leaseCheckOption = iota
	skipLeaseCheck
	normalLeaseCheck
)

const (
	respSuccResult         = "Download successful"
	userStorageType        = 4
	depotStorageType       = 5
	catalogSuffix          = "Catalog"
	expirationStringLayout = "2006-01-02 15:04:05.999999"
)

type nmaDownloadFileOp struct {
	opBase
	hostRequestBodyMap map[string]string
	// vdb will be used to save downloaded file info for revive_db
	vdb *VCoordinationDatabase
	// newNodes is used to verify node number in http response for revive_db
	newNodes           []string
	displayOnly        bool
	ignoreClusterLease bool
	forRevive          bool
	leaseCheckOption   leaseCheckOption
}

type downloadFileRequestData struct {
	SourceFilePath      string            `json:"source_file_path"`
	DestinationFilePath string            `json:"destination_file_path"`
	CatalogPath         string            `json:"catalog_path,omitempty"`
	AWSAccessKeyID      string            `json:"aws_access_key_id,omitempty"`
	AWSSecretAccessKey  string            `json:"aws_secret_access_key,omitempty"`
	Parameters          map[string]string `json:"parameters,omitempty"`
}

// ClusterLeaseNotExpiredError is returned when you attempt to access a
// communal storage location when there is an active cluster lease on it.
type ClusterLeaseNotExpiredError struct {
	Expiration string
}

func (e *ClusterLeaseNotExpiredError) Error() string {
	return fmt.Sprintf("revive database cannot continue because the communal storage location might still be in use."+
		" The cluster lease will expire at %s(UTC)."+
		" Please ensure that the other cluster has stopped and try revive_db after the cluster lease expiration",
		e.Expiration)
}

// ReviveDBNodeCountMismatchError is the error that is returned when the number of
// nodes in the revived cluster does not match the number of nodes in the original cluster.
type ReviveDBNodeCountMismatchError struct {
	ReviveDBStep  string
	FailureHost   string
	NumOfNewNodes int
	NumOfOldNodes int
}

func (e *ReviveDBNodeCountMismatchError) Error() string {
	return fmt.Sprintf(`[%s] nodes mismatch found on host %s: the number of the new nodes in --hosts is %d,`+
		` but the number of primary nodes in the description file is %d`,
		e.ReviveDBStep, e.FailureHost, e.NumOfNewNodes, e.NumOfOldNodes)
}

func makeNMADownloadFileOp(newNodes []string, sourceFilePath, destinationFilePath, catalogPath string,
	configurationParameters map[string]string, vdb *VCoordinationDatabase) (nmaDownloadFileOp, error) {
	op := nmaDownloadFileOp{}
	op.name = "NMADownloadFileOp"
	op.description = fmt.Sprintf("Download %s", filepath.Base(sourceFilePath))
	initiator := getInitiator(newNodes)
	op.hosts = []string{initiator}
	op.vdb = vdb
	op.newNodes = newNodes

	// make https json data
	op.hostRequestBodyMap = make(map[string]string)
	for _, host := range op.hosts {
		requestData := downloadFileRequestData{}
		requestData.SourceFilePath = sourceFilePath
		requestData.DestinationFilePath = destinationFilePath
		requestData.CatalogPath = catalogPath
		requestData.Parameters = configurationParameters

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return op, fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return op, nil
}

func makeNMADownloadFileOpForRevive(newNodes []string, sourceFilePath, destinationFilePath, catalogPath string,
	configurationParameters map[string]string, vdb *VCoordinationDatabase, displayOnly, ignoreClusterLease bool) (nmaDownloadFileOp, error) {
	op, err := makeNMADownloadFileOp(newNodes, sourceFilePath, destinationFilePath,
		catalogPath, configurationParameters, vdb)
	if err != nil {
		return op, err
	}
	op.displayOnly = displayOnly
	op.ignoreClusterLease = ignoreClusterLease
	op.forRevive = true
	op.leaseCheckOption = normalLeaseCheck

	return op, nil
}

func makeNMADownloadFileOpForRestore(newNodes []string, sourceFilePath, destinationFilePath, catalogPath string,
	configurationParameters map[string]string, vdb *VCoordinationDatabase, displayOnly bool) (nmaDownloadFileOp, error) {
	op, err := makeNMADownloadFileOpForRevive(newNodes, sourceFilePath, destinationFilePath,
		catalogPath, configurationParameters, vdb, displayOnly, true)
	if err != nil {
		return op, err
	}
	op.leaseCheckOption = skipLeaseCheck

	return op, nil
}

func makeNMADownloadFileOpForRestoreLeaseCheck(newNodes []string, sourceFilePath, destinationFilePath,
	catalogPath string, configurationParameters map[string]string,
	vdb *VCoordinationDatabase, ignoreClusterLease bool) (nmaDownloadFileOp, error) {
	op, err := makeNMADownloadFileOpForRevive(newNodes, sourceFilePath, destinationFilePath,
		catalogPath, configurationParameters, vdb, false, ignoreClusterLease)
	if err != nil {
		return op, err
	}
	op.leaseCheckOption = leaseCheckOnly

	return op, nil
}

func (op *nmaDownloadFileOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("vertica/download-file")
		httpRequest.RequestData = op.hostRequestBodyMap[host]

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaDownloadFileOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaDownloadFileOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaDownloadFileOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type downloadResponse struct {
	Result      string `json:"std_out"`
	FileContent string `json:"file_content"`
}

type fileContent struct {
	ClusterLeaseExpiration string `json:"ClusterLeaseExpiration"`
	NodeList               []struct {
		Name        string `json:"name"`
		Address     string `json:"address"`
		CatalogPath string `json:"catalogPath"`
		IsPrimary   bool   `json:"isPrimary"`
	} `json:"Node"`
	StorageLocations []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Usage int    `json:"usage"`
	} `json:"StorageLocation"`
	Sandbox string
}

func (op *nmaDownloadFileOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			response := downloadResponse{}
			err := op.parseAndCheckResponse(host, result.content, &response)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				break
			}

			result := strings.TrimSpace(response.Result)
			if result != respSuccResult {
				err = fmt.Errorf(`[%s] fail to download file on host %s, error result in the response is %s`, op.name, host, result)
				op.logger.Error(err, "fail to download file, detail")
				allErrs = errors.Join(allErrs, err)
				break
			}

			// for --display-only, we only need the file content
			if op.displayOnly && op.forRevive {
				execContext.dbInfo = response.FileContent
				return nil
			}

			// file content in the response is a string, we need to unmarshal it again
			descFileContent := fileContent{}
			err = op.parseAndCheckResponse(host, response.FileContent, &descFileContent)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				break
			}

			if op.forRevive {
				if op.leaseCheckOption != skipLeaseCheck {
					err = op.clusterLeaseCheck(descFileContent.ClusterLeaseExpiration)
					if err != nil {
						allErrs = errors.Join(allErrs, err)
						break
					}
				}

				if op.leaseCheckOption == leaseCheckOnly {
					return nil
				}

				// if users provide a subset of nodes for reviving,
				// we assume users intend to revive to primary subclusters
				if len(descFileContent.NodeList) > len(op.newNodes) {
					filterPrimaryNodes(&descFileContent)
				}

				if len(descFileContent.NodeList) != len(op.newNodes) {
					err := &ReviveDBNodeCountMismatchError{
						ReviveDBStep:  op.name,
						FailureHost:   host,
						NumOfNewNodes: len(op.newNodes),
						NumOfOldNodes: len(descFileContent.NodeList),
					}
					allErrs = errors.Join(allErrs, err)
					break
				}
			}

			// save descFileContent in vdb
			return op.buildVDBFromClusterConfig(&descFileContent)
		}

		httpsErr := errors.Join(fmt.Errorf("[%s] HTTPS call failed on host %s", op.name, host), result.err)
		allErrs = errors.Join(allErrs, httpsErr)
	}

	return appendHTTPSFailureError(allErrs)
}

func filterPrimaryNodes(descFileContent *fileContent) {
	var updatedFileContent fileContent
	for _, node := range descFileContent.NodeList {
		if node.IsPrimary {
			updatedFileContent.NodeList = append(updatedFileContent.NodeList, node)
		}
	}
	descFileContent.NodeList = updatedFileContent.NodeList
}

// buildVDBFromClusterConfig can build a vdb using cluster_config.json
func (op *nmaDownloadFileOp) buildVDBFromClusterConfig(descFileContent *fileContent) error {
	op.vdb.HostNodeMap = makeVHostNodeMap()
	for _, node := range descFileContent.NodeList {
		vNode := makeVCoordinationNode()
		vNode.Name = node.Name
		vNode.Address = node.Address
		vNode.IsPrimary = node.IsPrimary
		vNode.Sandbox = descFileContent.Sandbox

		// remove suffix "/Catalog" from node catalog path
		// e.g. /data/test_db/v_test_db_node0002_catalog/Catalog -> /data/test_db/v_test_db_node0002_catalog
		if filepath.Base(node.CatalogPath) == catalogSuffix {
			vNode.CatalogPath = filepath.Dir(node.CatalogPath)
		} else {
			vNode.CatalogPath = node.CatalogPath
		}

		for _, storage := range descFileContent.StorageLocations {
			// when storage name contains the node name, we know this storage is for that node
			// an example of storage name: "__location_1_v_test_db_node0001"
			// this will filter out communal storage location
			if strings.Contains(storage.Name, node.Name) {
				// we separate depot path and other storage locations
				if storage.Usage == depotStorageType {
					vNode.DepotPath = storage.Path
				} else {
					vNode.StorageLocations = append(vNode.StorageLocations, storage.Path)
					// we store the user storage location for later prepare directory use
					if storage.Usage == userStorageType {
						vNode.UserStorageLocations = append(vNode.UserStorageLocations, storage.Path)
					}
				}
			}
		}

		err := op.vdb.addNode(&vNode)
		if err != nil {
			return err
		}
	}

	return nil
}

func (op *nmaDownloadFileOp) clusterLeaseCheck(clusterLeaseExpiration string) error {
	if op.ignoreClusterLease {
		op.logger.PrintWarning("Skipping cluster lease check")
		return nil
	}

	utcExpiration, err := time.Parse(expirationStringLayout, clusterLeaseExpiration)
	if err != nil {
		wrappedErr := fmt.Errorf("fail to convert cluster-lease-expiration string to a time: %w", err)
		return wrappedErr
	}
	utcNow := time.Now().UTC()

	// current time < expire time, it means that the cluster lease is not expired
	if utcNow.Before(utcExpiration) {
		return &ClusterLeaseNotExpiredError{Expiration: clusterLeaseExpiration}
	}

	op.logger.PrintInfo("Cluster lease check has passed. We proceed to revive the database")
	return nil
}
