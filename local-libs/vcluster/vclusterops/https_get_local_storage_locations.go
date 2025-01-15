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
)

type httpsGetStorageLocsOp struct {
	opBase
	opHTTPSBase
	hostsWithNodeDetails hostNodeDetailsMap
}

func makeHTTPSGetStorageLocsOp(hosts []string, useHTTPPassword bool, userName string,
	httpsPassword *string, hostsWithNodeDetails hostNodeDetailsMap) (httpsGetStorageLocsOp, error) {
	op := httpsGetStorageLocsOp{}
	op.name = "HTTPSGetStorageLocsOp"
	op.description = "Get local node's storage locations"
	op.useHTTPPassword = useHTTPPassword
	if useHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
		if err != nil {
			return op, err
		}
		op.userName = userName
		op.httpsPassword = httpsPassword
	}
	op.hosts = hosts
	op.hostsWithNodeDetails = hostsWithNodeDetails
	return op, nil
}

func (op *httpsGetStorageLocsOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("node/storage-locations")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsGetStorageLocsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsGetStorageLocsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsGetStorageLocsOp) processResult(_ *opEngineExecContext) error {
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isPassing() {
			// we need to collect storage locations for all nodes, if one host failed to collect the info,
			// we consider the operation failed.
			return result.err
		}

		// decode the json-format response
		// The successful response will contain one node's storage locations:
		/*
			{
			  "storage_location_list": [
				{
				  "name": "__location_0_v_test_db_node0001",
				  "location_id": 45035996273705024,
				  "label": "",
				  "location_usage_type": "DATA,TEMP",
				  "location_path": "\/data\/test_db\/v_test_db_node0001_data",
				  "location_sharing_type": "NONE",
				  "max_size": 0,
				  "disk_percent": "",
				  "has_catalog": false,
				  "retired": false
				},
				{
				  "name": "__location_1_v_test_db_node0001",
				  "location_id": 45035996273705166,
				  "label": "auto-data-depot",
				  "location_usage_type": "DEPOT",
				  "location_path": "\/data\/test_db\/v_test_db_node0001_depot",
				  "location_sharing_type": "NONE",
				  "max_size": 8215897325568,
				  "disk_percent": "60%",
				  "has_catalog": false,
				  "retired": false
				}
			  ]
			}
		*/
		storageLocs := StorageLocations{}
		err := op.parseAndCheckResponse(host, result.content, &storageLocs)
		if err != nil {
			return fmt.Errorf(`[%s] failed to parse result on host %s, details: %w`, op.name, host, err)
		}

		// verify if the endpoint returns correct node info
		if len(storageLocs.StorageLocList) == 0 {
			return fmt.Errorf(`[%s] response should contain at least one storage location on host %s`, op.name, host)
		}

		// collect storage locations
		if nodeDetails, ok := op.hostsWithNodeDetails[host]; ok {
			nodeDetails.StorageLocations = storageLocs
		} else {
			// this is a programming error, the host should've been added to the map in HTTPSGetLocalNodeStateOp
			return fmt.Errorf(`[%s] found an unexpected host %s`, op.name, host)
		}
	}

	return nil
}

func (op *httpsGetStorageLocsOp) finalize(_ *opEngineExecContext) error {
	return nil
}
