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

type httpsGetClusterInfoOp struct {
	opBase
	opHTTPSBase
	dbName string
	vdb    *VCoordinationDatabase
}

func makeHTTPSGetClusterInfoOp(dbName string, hosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string, vdb *VCoordinationDatabase,
) (httpsGetClusterInfoOp, error) {
	op := httpsGetClusterInfoOp{}
	op.name = "HTTPSGetClusterInfoOp"
	op.description = "Collect cluster information"
	op.dbName = dbName
	op.hosts = hosts
	op.vdb = vdb
	op.useHTTPPassword = useHTTPPassword

	if useHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
		if err != nil {
			return op, err
		}
		op.userName = userName
		op.httpsPassword = httpsPassword
	}

	return op, nil
}

func (op *httpsGetClusterInfoOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("cluster")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsGetClusterInfoOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsGetClusterInfoOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

type clusterStateInfo struct {
	IsEon                    bool     `json:"is_eon"`
	DBName                   string   `json:"db_name"`
	CommunalStorageLocations []string `json:"commnual_storage_locations"`
}

func (op *httpsGetClusterInfoOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong password/certificate for https service on host %s",
				op.name, host)
		}

		if result.isPassing() {
			// unmarshal the response content
			clusterState := clusterStateInfo{}
			err := op.parseAndCheckResponse(host, result.content, &clusterState)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				return appendHTTPSFailureError(allErrs)
			}

			// save cluster info to vdb
			op.vdb.IsEon = clusterState.IsEon
			op.vdb.UseDepot = clusterState.IsEon
			op.vdb.Name = clusterState.DBName
			if op.vdb.Name != op.dbName {
				err = fmt.Errorf(`[%s] database %s is running on host %s, rather than database %s`, op.name, op.vdb.Name, host, op.dbName)
				allErrs = errors.Join(allErrs, err)
				break
			}
			if len(clusterState.CommunalStorageLocations) > 0 {
				op.vdb.CommunalStorageLocation = clusterState.CommunalStorageLocations[0]
			}
			return nil
		}
		allErrs = errors.Join(allErrs, result.err)
	}
	return appendHTTPSFailureError(allErrs)
}

func (op *httpsGetClusterInfoOp) finalize(_ *opEngineExecContext) error {
	return nil
}
