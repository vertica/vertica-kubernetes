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

type httpsStartReplicationOp struct {
	opBase
	opHTTPSBase
	TargetDB           DatabaseOptions
	hostRequestBodyMap map[string]string
	sourceDB           string
	targetHost         string
	sandbox            string
	tlsConfig          string
	vdb                *VCoordinationDatabase
}

func makeHTTPSStartReplicationOp(dbName string, sourceHosts []string,
	sourceUseHTTPPassword bool, sourceUserName string,
	sourceHTTPPassword *string, targetUseHTTPPassword bool, targetDBOpt *DatabaseOptions,
	targetHost string, tlsConfig, sandbox string, vdb *VCoordinationDatabase) (httpsStartReplicationOp, error) {
	op := httpsStartReplicationOp{}
	op.name = "HTTPSStartReplicationOp"
	op.description = "Start database replication"
	op.sourceDB = dbName
	op.hosts = sourceHosts
	op.useHTTPPassword = sourceUseHTTPPassword
	op.TargetDB.DBName = targetDBOpt.DBName
	op.targetHost = targetHost
	op.tlsConfig = tlsConfig
	op.sandbox = sandbox
	op.vdb = vdb

	if sourceUseHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, sourceUseHTTPPassword, sourceUserName)
		if err != nil {
			return op, err
		}
		op.userName = sourceUserName
		op.httpsPassword = sourceHTTPPassword
	}
	if targetUseHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, targetUseHTTPPassword, targetDBOpt.UserName)
		if err != nil {
			return op, err
		}
		op.TargetDB.UserName = targetDBOpt.UserName
		op.TargetDB.Password = targetDBOpt.Password
	}

	return op, nil
}

type replicateRequestData struct {
	TargetHost     string  `json:"host"`
	TargetDB       string  `json:"dbname"`
	TargetUserName string  `json:"user,omitempty"`
	TargetPassword *string `json:"password,omitempty"`
	TLSConfig      string  `json:"tls_config,omitempty"`
}

func (op *httpsStartReplicationOp) setupRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		replicateData := replicateRequestData{}
		replicateData.TargetHost = op.targetHost
		replicateData.TargetDB = op.TargetDB.DBName
		replicateData.TargetUserName = op.TargetDB.UserName
		replicateData.TargetPassword = op.TargetDB.Password
		replicateData.TLSConfig = op.tlsConfig

		dataBytes, err := json.Marshal(replicateData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *httpsStartReplicationOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint("replicate/start")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}
	return nil
}

func (op *httpsStartReplicationOp) prepare(execContext *opEngineExecContext) error {
	sourceHost, err := getInitiatorHostForReplication(op.name, op.sandbox, op.hosts, op.vdb)
	if err != nil {
		return err
	}
	// use first up host to execute https post request
	op.hosts = sourceHost

	err = op.setupRequestBody(op.hosts)
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsStartReplicationOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsStartReplicationOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			// skip checking response from other nodes because we will get the same error there
			return result.err
		}
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// decode the json-format response
		// The successful response object will be a dictionary as below:
		// {"detail": "REPLICATE"}
		startRepRsp, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			err = fmt.Errorf("[%s] fail to parse result on host %s, details: %w", op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// verify if the response's content is correct
		const startReplicationOpSuccMsg = "REPLICATE"
		if startRepRsp["detail"] != startReplicationOpSuccMsg {
			err = fmt.Errorf(`[%s] response detail should be '%s' but got '%s'`, op.name, startReplicationOpSuccMsg, startRepRsp["detail"])
			allErrs = errors.Join(allErrs, err)
		}
	}

	return allErrs
}

func (op *httpsStartReplicationOp) finalize(_ *opEngineExecContext) error {
	return nil
}
