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

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaReplicationStartOp struct {
	opBase
	nmaStartReplicationRequestData
	hostRequestBodyMap map[string]string
	sandbox            string
	vdb                *VCoordinationDatabase
}

func makeNMAReplicationStartOp(sourceHosts []string,
	sourceUsePassword bool, targetUsePassword bool,
	replicationRequestData *nmaStartReplicationRequestData,
	vdb *VCoordinationDatabase) (nmaReplicationStartOp, error) {
	op := nmaReplicationStartOp{}
	op.name = "NMAReplicationStartOp"
	op.description = "Start asynchronous database replication"
	op.hosts = sourceHosts
	op.nmaStartReplicationRequestData = *replicationRequestData
	op.vdb = vdb

	if sourceUsePassword {
		err := util.ValidateUsernameAndPassword(op.name, sourceUsePassword, replicationRequestData.Username)
		if err != nil {
			return op, err
		}
		op.Username = replicationRequestData.Username
		op.Password = replicationRequestData.Password
	}
	if targetUsePassword {
		err := util.ValidateUsernameAndPassword(op.name, targetUsePassword, replicationRequestData.TargetUserName)
		if err != nil {
			return op, err
		}
		op.TargetUserName = replicationRequestData.TargetUserName
		op.TargetPassword = replicationRequestData.TargetPassword
	}

	return op, nil
}

type nmaStartReplicationRequestData struct {
	DBName            string  `json:"dbname"`
	ExcludePattern    string  `json:"exclude_pattern,omitempty"`
	IncludePattern    string  `json:"include_pattern,omitempty"`
	TableOrSchemaName string  `json:"table_or_schema_name,omitempty"`
	Username          string  `json:"username"`
	Password          *string `json:"password"`
	TargetDBName      string  `json:"target_dbname"`
	TargetHost        string  `json:"target_hostname"`
	TargetNamespace   string  `json:"target_namespace,omitempty"`
	TargetUserName    string  `json:"target_username,omitempty"`
	TargetPassword    *string `json:"target_password,omitempty"`
	TLSConfig         string  `json:"tls_config,omitempty"`
}

func (op *nmaReplicationStartOp) updateRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		dataBytes, err := json.Marshal(op.nmaStartReplicationRequestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaReplicationStartOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("replicate/start")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaReplicationStartOp) prepare(execContext *opEngineExecContext) error {
	sourceHost, err := getInitiatorHostForReplication(op.name, op.sandbox, op.hosts, op.vdb)
	if err != nil {
		return err
	}
	// use first up host to execute https post request
	op.hosts = sourceHost

	err = op.updateRequestBody(op.hosts)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaReplicationStartOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaReplicationStartOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaReplicationStartOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong certificate for NMA service on host %s",
				op.name, host)
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
