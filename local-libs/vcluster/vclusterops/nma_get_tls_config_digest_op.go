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

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type nmaGetTLSConfigDigestOp struct {
	opBase
	hostRequestBody   string
	initiator         string
	expectedTLSConfig *tlsConfigInfo
	logger            vlog.Printer
}

type getTLSConfigDigestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

type TLSConfigDigest struct {
	Digest string `json:"get_tls_config_digest"`
}

type TLSConfigDigestResponse []TLSConfigDigest

func makeNMAGetTLSConfigDigestOp(hosts []string,
	username, dbName, tlsConfigName string, /* out parameter */
	password *string, useHTTPPassword bool, tlsConfigHolder *tlsConfigInfo, logger vlog.Printer) (nmaGetTLSConfigDigestOp, error) {
	op := nmaGetTLSConfigDigestOp{}
	op.name = "NMAGetTLSConfigDigestOp"
	op.description = "Get TLS config digest"
	op.hosts = hosts
	op.logger = logger
	err := op.setupRequestBody(username, dbName, tlsConfigName, password, useHTTPPassword)
	if err != nil {
		return op, err
	}
	if tlsConfigHolder == nil {
		// really an assertion - this should never fail
		return op, errors.New("cannot hold tls config info")
	}
	op.expectedTLSConfig = tlsConfigHolder

	return op, nil
}

func (op *nmaGetTLSConfigDigestOp) setupRequestBody(
	username, dbName, tlsConfigName string, password *string,
	useDBPassword bool) error {
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	getTLSConfigDigestData := &getTLSConfigDigestData{}
	getTLSConfigDigestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	getTLSConfigDigestData.Params = make(map[string]any)
	getTLSConfigDigestData.Params["tls-config-name"] = tlsConfigName
	dataBytes, err := json.Marshal(getTLSConfigDigestData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}
	op.hostRequestBody = string(dataBytes)
	op.logger.Info("request data", "op name", op.name, "hostRequestBody", op.hostRequestBody)
	return nil
}

func (op *nmaGetTLSConfigDigestOp) setupClusterHTTPRequest(initiator string) error {
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PostMethod
	httpRequest.buildNMAEndpoint("vertica/tls_digest")
	httpRequest.RequestData = op.hostRequestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest

	return nil
}

func (op *nmaGetTLSConfigDigestOp) prepare(execContext *opEngineExecContext) error {
	// select an up host in the sandbox or main cluster as the initiator
	initiator, err := getInitiatorHost(op.hosts, []string{})
	if err != nil {
		return err
	}
	op.initiator = initiator
	execContext.dispatcher.setup([]string{op.initiator})
	return op.setupClusterHTTPRequest(op.initiator)
}

func (op *nmaGetTLSConfigDigestOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaGetTLSConfigDigestOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaGetTLSConfigDigestOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			var digestResponse TLSConfigDigestResponse
			err := json.Unmarshal([]byte(result.content), &digestResponse)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
			if len(digestResponse) == 0 {
				allErrs = errors.Join(allErrs, errors.New("digest is missing from response"))
			}
			op.expectedTLSConfig.Digest = digestResponse[0].Digest
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}
	return allErrs
}
