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

type nmaBootstrapCatalogOp struct {
	opBase
	hostRequestBodyMap      map[string]bootstrapCatalogRequestData
	marshaledRequestBodyMap map[string]string
}

type bootstrapCatalogRequestData struct {
	DBName             string `json:"db_name"`
	Host               string `json:"host"`
	NodeName           string `json:"node_name"`
	CatalogPath        string `json:"catalog_path"`
	StorageLocation    string `json:"storage_location"`
	PortNumber         int    `json:"port_number"`
	ControlAddr        string `json:"control_addr"`
	BroadcastAddr      string `json:"broadcast_addr"`
	LicenseKey         string `json:"license_key"`
	ControlPort        string `json:"spread_port"`
	LargeCluster       int    `json:"large_cluster"`
	NetworkingMode     string `json:"networking_mode"`
	SpreadLogging      bool   `json:"spread_logging"`
	SpreadLoggingLevel int    `json:"spread_logging_level"`
	Ipv6               bool   `json:"ipv6"`
	NumShards          int    `json:"num_shards"`
	CommunalStorageURL string `json:"communal_storage"`
	SuperuserName      string `json:"superuser_name"`
	GenerateHTTPCerts  bool   `json:"generate_http_certs"`
	sensitiveFields
}

func makeNMABootstrapCatalogOp(
	vdb *VCoordinationDatabase,
	options *VCreateDatabaseOptions,
	bootstrapHosts []string) (nmaBootstrapCatalogOp, error) {
	op := nmaBootstrapCatalogOp{}
	op.name = "NMABootstrapCatalogOp"
	op.description = "Bootstrap catalog"
	// usually, only one node need bootstrap catalog
	op.hosts = bootstrapHosts

	err := op.setupRequestBody(vdb, options)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaBootstrapCatalogOp) setupRequestBody(vdb *VCoordinationDatabase, options *VCreateDatabaseOptions) error {
	op.hostRequestBodyMap = make(map[string]bootstrapCatalogRequestData)

	for _, host := range op.hosts {
		bootstrapData := bootstrapCatalogRequestData{}
		bootstrapData.DBName = vdb.Name

		vnode := vdb.HostNodeMap[host]
		bootstrapData.Host = host
		bootstrapData.NodeName = vnode.Name
		bootstrapData.CatalogPath = vnode.CatalogPath
		if len(vnode.StorageLocations) == 0 {
			return fmt.Errorf("[%s] the storage locations is empty", op.name)
		}
		bootstrapData.StorageLocation = vnode.StorageLocations[0]

		// client port: spread port will be computed based on client port
		bootstrapData.PortNumber = vnode.Port
		bootstrapData.Parameters = options.ConfigurationParameters

		// need to read network_profile info in execContext
		// see execContext in nmaBootstrapCatalogOp:prepare()
		bootstrapData.ControlAddr = vnode.Address

		bootstrapData.LicenseKey = vdb.LicensePathOnNode
		// large cluster mode temporariliy disabled
		bootstrapData.LargeCluster = options.LargeCluster
		if options.P2p {
			bootstrapData.NetworkingMode = "pt2pt"
		} else {
			bootstrapData.NetworkingMode = "broadcast"
		}
		bootstrapData.SpreadLogging = options.SpreadLogging
		bootstrapData.SpreadLoggingLevel = options.SpreadLoggingLevel
		bootstrapData.Ipv6 = options.IPv6
		bootstrapData.SuperuserName = options.UserName
		bootstrapData.DBPassword = *options.Password

		// Flag to generate certs and tls configuration
		bootstrapData.GenerateHTTPCerts = options.GenerateHTTPCerts

		// Eon params
		bootstrapData.NumShards = vdb.NumShards
		bootstrapData.CommunalStorageURL = vdb.CommunalStorageLocation
		bootstrapData.AWSAccessKeyID = vdb.AwsIDKey
		bootstrapData.AWSSecretAccessKey = vdb.AwsSecretKey

		op.hostRequestBodyMap[host] = bootstrapData
	}

	return nil
}

func (op *nmaBootstrapCatalogOp) updateRequestBody(execContext *opEngineExecContext) error {
	op.marshaledRequestBodyMap = make(map[string]string)
	maskedRequestBodyMap := make(map[string]bootstrapCatalogRequestData)

	// update request body from network profiles
	for host, profile := range execContext.networkProfiles {
		requestBody := op.hostRequestBodyMap[host]
		requestBody.BroadcastAddr = profile.Broadcast
		op.hostRequestBodyMap[host] = requestBody

		dataBytes, err := json.Marshal(op.hostRequestBodyMap[host])
		if err != nil {
			op.logger.Error(err, `[%s] fail to marshal request data to JSON string`, op.name)
			return err
		}
		op.marshaledRequestBodyMap[host] = string(dataBytes)

		// mask sensitive data for logs
		maskedData := requestBody
		maskedData.maskSensitiveInfo()
		maskedRequestBodyMap[host] = maskedData
	}
	op.logger.Info("request data", "op name", op.name, "bodyMap", maskedRequestBodyMap)

	return nil
}

func (op *nmaBootstrapCatalogOp) setupClusterHTTPRequest(hosts []string) error {
	// usually, only one node need bootstrap catalog
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("catalog/bootstrap")
		httpRequest.RequestData = op.marshaledRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaBootstrapCatalogOp) prepare(execContext *opEngineExecContext) error {
	err := op.updateRequestBody(execContext)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaBootstrapCatalogOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaBootstrapCatalogOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaBootstrapCatalogOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			// the response object will be a dictionary, e.g.,:
			// {'bootstrap_catalog_stdout':  'Catalog successfully bootstrapped',
			// 'bootstrap_catalog_stderr':'',
			// 'bootstrap_catalog_return_code', '0'}

			responseMap, err := op.parseAndCheckMapResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}

			code, ok := responseMap["bootstrap_catalog_return_code"]
			if !ok {
				err = fmt.Errorf(`[%s] response does not contain the field "bootstrap_catalog_return_code"`, op.name)
				allErrs = errors.Join(allErrs, err)
				continue
			}
			if code != "0" {
				err = fmt.Errorf(`[%s] bootstrap_catalog_return_code should be 0 but got %s`, op.name, code)
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
