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
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type nmaSpreadSecurityOp struct {
	opBase
	catalogPathMap map[string]string
	keyType        string
}

type nmaSpreadSecurityPayload struct {
	CatalogPath           string `json:"catalog_path"`
	SpreadSecurityDetails string `json:"spread_security_details"`
}

const spreadKeyTypeVertica = "vertica"

// makeNMASpreadSecurityOp will create the op to set or rotate the key for
// spread encryption.
func makeNMASpreadSecurityOp(
	logger vlog.Printer,
	keyType string,
) nmaSpreadSecurityOp {
	return nmaSpreadSecurityOp{
		opBase: opBase{
			logger:      logger.WithName("NMASpreadSecurityOp"),
			name:        "NMASpreadSecurityOp",
			description: "Set new spread encryption key",
			hosts:       nil, // We always set this at runtime from read catalog editor
		},
		catalogPathMap: nil, // Set at runtime after reading the catalog editor
		keyType:        keyType,
	}
}

func (op *nmaSpreadSecurityOp) setupRequestBody() (map[string]string, error) {
	if len(op.hosts) == 0 {
		return nil, fmt.Errorf("[%s] no hosts specified", op.name)
	}

	// Get the spread encryption key. Never write the contents of securityDetails
	// to a log or error message. Otherwise, we risk leaking the key.
	securityDetails, err := op.generateSecurityDetails()
	if err != nil {
		return nil, err
	}

	hostRequestBodyMap := make(map[string]string, len(op.hosts))
	for _, host := range op.hosts {
		fullCatalogPath, ok := op.catalogPathMap[host]
		if !ok {
			return nil, fmt.Errorf("could not find host %s in catalogPathMap %v", host, op.catalogPathMap)
		}
		payload := nmaSpreadSecurityPayload{
			CatalogPath:           getCatalogPath(fullCatalogPath),
			SpreadSecurityDetails: securityDetails,
		}

		dataBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("[%s] fail to marshal payload data into JSON string, detail %w", op.name, err)
		}

		hostRequestBodyMap[host] = string(dataBytes)
	}
	return hostRequestBodyMap, nil
}

func (op *nmaSpreadSecurityOp) setupClusterHTTPRequest(hostRequestBodyMap map[string]string) error {
	for host, requestBody := range hostRequestBodyMap {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("catalog/spread-security")
		httpRequest.RequestData = requestBody
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaSpreadSecurityOp) prepare(execContext *opEngineExecContext) error {
	if err := op.setRuntimeParms(execContext); err != nil {
		return err
	}
	hostRequestBodyMap, err := op.setupRequestBody()
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(hostRequestBodyMap)
}

func (op *nmaSpreadSecurityOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaSpreadSecurityOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaSpreadSecurityOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		// For a passing result, the response that comes back isn't JSON. So,
		// don't parse and validate it. Just check the status code. The non-JSON
		// response we get is: 'Written to spread.conf'. VER-89658 is opened
		// to change the endpoint to return JSON.
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
		}
	}
	return allErrs
}

// setRuntimeParms will set options based on runtime context.
func (op *nmaSpreadSecurityOp) setRuntimeParms(execContext *opEngineExecContext) error {
	// A core dump can happen if we send the /v1/catalog/spread-security settings to a secondary node.
	// Need to use the primary node because fetching global settings to perform a catalog lookup isn't available on a secondary node.
	// Always pull the hosts at runtime using the primary node with the latest catalog.
	// Need to use the ones with the latest catalog because those are the hosts
	// that we copy the spread.conf from during start db.
	hostsWithLatestCatalog := execContext.hostsWithLatestCatalog
	if len(hostsWithLatestCatalog) == 0 {
		return fmt.Errorf("could not find at least one host with the latest catalog")
	}
	// Use only a primary host with the latest catalog as the sourceConfigHost
	primaryHostsWithLatestCatalog := getPrimaryHostsWithLatestCatalog(&execContext.nmaVDatabase, hostsWithLatestCatalog, execContext)
	if len(primaryHostsWithLatestCatalog) == 0 {
		return fmt.Errorf("could not find at least one primary host with the latest catalog")
	}
	op.hosts = []string{primaryHostsWithLatestCatalog[0]}
	op.catalogPathMap = make(map[string]string, len(op.hosts))
	err := updateCatalogPathMapFromCatalogEditor(op.hosts, &execContext.nmaVDatabase, op.catalogPathMap)
	if err != nil {
		return fmt.Errorf("failed to get catalog paths from catalog editor: %w", err)
	}
	return nil
}

func (op *nmaSpreadSecurityOp) generateSecurityDetails() (string, error) {
	keyID, err := op.generateKeyID()
	if err != nil {
		return "", err
	}

	var spreadKey string
	switch op.keyType {
	case spreadKeyTypeVertica:
		spreadKey, err = op.generateVerticaSpreadKey()
		if err != nil {
			return "", err
		}
	default:
		// Note, there is another key type that we support in the server
		// (aws-kms). But we haven't yet added support for that here.
		// VER-89659 is opened to address that.
		return "", fmt.Errorf("unsupported spread key type %s", op.keyType)
	}
	// Note, we log the key ID for info purposes and is safe because it isn't
	// sensitive. NEVER log the spreadKey.
	op.logger.Info("generating spread key", "keyID", keyID)
	return fmt.Sprintf(`{\"%s\":\"%s\"}`, keyID, spreadKey), nil
}

func (op *nmaSpreadSecurityOp) generateVerticaSpreadKey() (string, error) {
	const spreadKeySize = 32
	bytes := make([]byte, spreadKeySize)
	if _, err := crand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes for spread: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func (op *nmaSpreadSecurityOp) generateKeyID() (string, error) {
	const keyLength = 2
	bytes := make([]byte, keyLength)
	if _, err := crand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes for key ID: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
