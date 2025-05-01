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
)

// nmaRotateHTTPSCertsOp will, via the NMA SQL proxy, rotate the cert,
// key, ca cert, and optionally tlsmode used by the HTTPS service if
// the new secrets are backed by a secrets manager.
// Since this is a catalog operation, the hosts list should have exactly
// one host per sandbox to keep all db groups in sync.
type nmaRotateHTTPSCertsOp struct {
	opBase
	hostRequestBody  string            // constructed by make function
	hostsToSandboxes map[string]string // for logging
}

type rotateHTTPSCertsData struct {
	sqlEndpointData
	RotateHTTPSCertsData
	// name of the secret manager, e.g. "KubernetesSecretManager"
	// currently only that, and can be moved to RotateHTTPSCertsData
	// to expose it to the caller of vclusterops
	SecretManager string `json:"secret_manager"` // required
}

type rotateHTTPSCertsResponse struct {
	// Catalog name of the key created by the rotation
	KeyName string `json:"key_name"`
	// Catalog name of the cert created by the rotation
	CertName string `json:"cert_name"`
	// Catalog name of the ca cert created by the rotation
	CACertName string `json:"ca_cert_name"`
	// Catalog name of the tls config object modified by the rotation
	TLSConfigName string `json:"tls_config_name"`
}

// makeNMARotateHTTPSCertsOp should be passed a host list of one initiator
// per sandbox to keep all db groups in sync (including main)
func makeNMARotateHTTPSCertsOp(hosts []string,
	username, dbName string,
	hostsToSandboxes map[string]string,
	opData *RotateHTTPSCertsData,
	password *string,
	useHTTPPassword bool) (nmaRotateHTTPSCertsOp, error) {
	op := nmaRotateHTTPSCertsOp{}
	op.name = "NMARotateHTTPSCertsOp"
	op.description = "Rotate the HTTPS service certificates"
	op.hosts = hosts
	op.hostsToSandboxes = hostsToSandboxes
	err := validateHostMapsAllowEmpty(hosts, op.hostsToSandboxes)
	if err != nil {
		return op, fmt.Errorf("could not find sandbox for each initiator host: %w", err)
	}
	err = op.setupRequestBody(username, dbName, opData, password, useHTTPPassword)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaRotateHTTPSCertsOp) setupRequestBody(
	username, dbName string,
	opData *RotateHTTPSCertsData,
	password *string,
	useDBPassword bool) error {
	err := ValidateSQLEndpointData(op.name, useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	endpointData := rotateHTTPSCertsData{}
	endpointData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	if opData == nil {
		return errors.New("argument opData cannot be a nil pointer")
	}
	endpointData.RotateHTTPSCertsData = *opData
	endpointData.SecretManager = "AWSSecretManager" // others NYI

	dataBytes, err := json.Marshal(endpointData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.hostRequestBody = string(dataBytes)

	op.logger.Info("request data", "op name", op.name, "hostRequestBody", op.hostRequestBody)

	return nil
}

func (op *nmaRotateHTTPSCertsOp) setupClusterHTTPRequest(hosts []string) error {
	// the request is the same for all hosts
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PostMethod
	httpRequest.buildNMAEndpoint("vertica/https/rotate-certs")
	httpRequest.RequestData = op.hostRequestBody
	for _, host := range hosts {
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}
	return nil
}

func (op *nmaRotateHTTPSCertsOp) prepare(execContext *opEngineExecContext) error {
	// the host list should already have been filtered to select initiators across all
	// db groups
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaRotateHTTPSCertsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaRotateHTTPSCertsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaRotateHTTPSCertsOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		if result.isPassing() {
			resp := rotateHTTPSCertsResponse{}
			err := op.parseAndCheckResponse(host, result.content, &resp)
			if err != nil {
				op.logResponse(host, result)
				allErrs = errors.Join(allErrs, err)
			}
			op.logger.Info("rotated https service certs", "host", host, "sandbox", op.hostsToSandboxes[host],
				"tlsConfig", resp.TLSConfigName, "key", resp.KeyName, "cert", resp.CertName, "caCert", resp.CACertName)
		} else {
			op.logResponse(host, result)
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
