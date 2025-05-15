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
	"regexp"
)

type nmaSetTLSOp struct {
	opBase
	hostRequestBody string
}

type nmaSetTLSRequestData struct {
	sqlEndpointData
	TLSNamespace         string `json:"k8s_tls_namespace"`
	TLSSecretName        string `json:"tls_secret_name"`
	TLSConfigName        string `json:"tls_config"`
	TLSKeyDataKey        string `json:"tls_key_data_key"`
	TLSCertDataKey       string `json:"tls_cert_data_key"`
	TLSCADataKey         string `json:"tls_ca_data_key"`
	TLSSecretManager     string `json:"tls_secret_manager"`
	TLSMode              string `json:"tls_mode"`
	TLSConfigGrantAuth   bool   `json:"tls_config_grant_auth,omitempty"`
	TLSConfigSyncCatalog bool   `json:"tls_config_sync_catalog,omitempty"`
	AWSRegion            string `json:"aws_region"`
	AWSSecretVersionID   string `json:"aws_secret_version_id"`
}

func makeNMASetTLSOp(options *DatabaseOptions, configName string,
	grantAuth, syncCatalog bool, configMap map[string]string) (nmaSetTLSOp, error) {
	op := nmaSetTLSOp{}
	op.name = "nmaSetTLSOp"
	op.description = "Set tls config"
	op.hosts = options.Hosts

	err := op.setupRequestBody(options.UserName, options.DBName, configName, options.Password,
		options.usePassword, grantAuth, syncCatalog, configMap)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaSetTLSOp) setupRequestBody(
	username, dbName, configName string, password *string, useDBPassword, grantAuth, syncCatalog bool, configMap map[string]string) error {
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	setConfigData := nmaSetTLSRequestData{}
	setConfigData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	setConfigData.TLSCADataKey = configMap[TLSSecretManagerKeyCACertDataKey]
	setConfigData.TLSCertDataKey = configMap[TLSSecretManagerKeyCertDataKey]
	setConfigData.TLSKeyDataKey = configMap[TLSSecretManagerKeyKeyDataKey]
	setConfigData.TLSConfigName = configName
	setConfigData.TLSNamespace = configMap[TLSSecretManagerKeyNamespace]
	setConfigData.TLSSecretName = configMap[TLSSecretManagerKeySecretName]
	setConfigData.TLSMode = genNMACompatibleTLSMode(configMap[TLSSecretManagerKeyTLSMode])
	setConfigData.TLSSecretManager = configMap[TLSSecretManagerKeySecretManager]
	setConfigData.AWSRegion = configMap[TLSSecretManagerKeyAWSRegion]
	setConfigData.AWSSecretVersionID = configMap[TLSSecretManagerKeyAWSSecretVersionID]
	setConfigData.TLSConfigGrantAuth = grantAuth
	setConfigData.TLSConfigSyncCatalog = syncCatalog

	dataBytes, err := json.Marshal(setConfigData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.hostRequestBody = string(dataBytes)

	op.logger.Info("request data", "op name", op.name, "hostRequestBody", op.hostRequestBody)

	return nil
}

func (op *nmaSetTLSOp) setupClusterHTTPRequest(hosts []string) error {
	initiator := getInitiator(hosts)
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PutMethod
	httpRequest.buildNMAEndpoint("vertica/tls")
	httpRequest.RequestData = op.hostRequestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest
	return nil
}

func (op *nmaSetTLSOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaSetTLSOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaSetTLSOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaSetTLSOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			_, err := op.parseAndCheckStringResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}

func genNMACompatibleTLSMode(tlsMode string) string {
	m := regexp.MustCompile(`_`)
	return m.ReplaceAllString(tlsMode, "-")
}
