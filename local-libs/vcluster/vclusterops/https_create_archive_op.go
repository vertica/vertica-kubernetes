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

type httpsCreateArchiveOp struct {
	opBase
	opHTTPSBase
	ArchiveName        string
	NumRestorePoints   int
	hostRequestBodyMap map[string]string
}

type createArchiveRequestData struct {
	NumRestorePoints int `json:"num_restore_points,omitempty"`
}

func (op *httpsCreateArchiveOp) setupRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		createArchiveData := createArchiveRequestData{}
		if op.NumRestorePoints != CreateArchiveDefaultNumRestore {
			createArchiveData.NumRestorePoints = op.NumRestorePoints
		}
		dataBytes, err := json.Marshal(createArchiveData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// makeHTTPSCreateArchiveOp will make an op that call vertica-http service to create archive for database
func makeHTTPSCreateArchiveOp(hosts []string, useHTTPPassword bool, userName string,
	httpsPassword *string, archiveName string, numRestorePoints int,
) (httpsCreateArchiveOp, error) {
	op := httpsCreateArchiveOp{}
	op.name = "HTTPSCreateArchiveOp"
	op.description = "Create archive for database"
	op.hosts = hosts
	op.useHTTPPassword = useHTTPPassword
	if useHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
		if err != nil {
			return op, err
		}
		op.userName = userName
		op.httpsPassword = httpsPassword
	}
	op.ArchiveName = archiveName
	op.NumRestorePoints = numRestorePoints
	return op, nil
}

func (op *httpsCreateArchiveOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.ArchiveEndpoint + "/" + op.ArchiveName)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsCreateArchiveOp) prepare(execContext *opEngineExecContext) error {
	err := op.setupRequestBody(op.hosts)
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsCreateArchiveOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsCreateArchiveOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	// every host needs to have a successful result, otherwise we fail this op
	// because we want archives to be created
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong password/certificate for https service on host %s",
				op.name, host)
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			// not break here because we want to log all the failed nodes
			continue
		}

		/* decode the json-format response
			The successful response object will be a dictionary like below:
			{
		  		"detail": ""
			}

		*/
		_, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			// not break here because we want to log all the failed nodes
			continue
		}
	}
	return allErrs
}

func (op *httpsCreateArchiveOp) finalize(_ *opEngineExecContext) error {
	return nil
}
