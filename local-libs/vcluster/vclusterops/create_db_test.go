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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

const defaultPath = "/data"

func TestValidateDepotSize(t *testing.T) {
	res, err := validateDepotSize("-19%")
	assert.Equal(t, res, false)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "it is less than 0%")

	res, err = validateDepotSize("119%")
	assert.Equal(t, res, false)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "it is greater than 100%")

	res, err = validateDepotSize("+19%")
	assert.Equal(t, res, true)
	assert.Nil(t, err)

	res, err = validateDepotSize("19%")
	assert.Equal(t, res, true)
	assert.Nil(t, err)

	res, err = validateDepotSize("-119K")
	assert.Equal(t, res, false)
	assert.NotNil(t, err)
	assert.ErrorContains(t, err, "it is <= 0")

	res, err = validateDepotSize("+119T")
	assert.Equal(t, res, true)
	assert.Nil(t, err)
}

func TestValidateTLSConfigurationMap(t *testing.T) {
	logger := vlog.Printer{}

	// Positive test: valid Kubernetes config with verify-full mode
	validConfig := map[string]string{
		TLSSecretManagerKeySecretName:    "my-secret",
		TLSSecretManagerKeySecretManager: K8sSecretManagerType,
		TLSSecretManagerKeyNamespace:     "default",
		TLSSecretManagerKeyTLSMode:       string(tlsModeVerifyFull),
		TLSSecretManagerKeyKeyDataKey:    "key-data",
		TLSSecretManagerKeyCertDataKey:   "cert-data",
		TLSSecretManagerKeyCACertDataKey: "ca-cert-data",
	}
	err := validateTLSConfigurationMap(validConfig, "https", logger)
	assert.Nil(t, err)

	// Negative: missing TLSSecretManagerKeySecretName
	configMissingSecretName := cloneMap(validConfig)
	delete(configMissingSecretName, TLSSecretManagerKeySecretName)
	err = validateTLSConfigurationMap(configMissingSecretName, "https", logger)
	assert.ErrorContains(t, err, fmt.Sprintf("the %s key must exist with a non-empty value", TLSSecretManagerKeySecretName))

	// Negative: invalid secret manager
	configInvalidSM := cloneMap(validConfig)
	configInvalidSM[TLSSecretManagerKeySecretManager] = "invalid"
	err = validateTLSConfigurationMap(configInvalidSM, "https", logger)
	assert.ErrorContains(t, err, fmt.Sprintf("the %s key must exist and its value must be one of", TLSSecretManagerKeySecretManager))

	// Negative: missing required k8s namespace
	configMissingNS := cloneMap(validConfig)
	delete(configMissingNS, TLSSecretManagerKeyNamespace)
	err = validateTLSConfigurationMap(configMissingNS, "https", logger)
	assert.ErrorContains(t, err, fmt.Sprintf("when the secret manager is %s, the %s key is required",
		K8sSecretManagerType, TLSSecretManagerKeyNamespace))

	// Positive test: valid AWS config with enable mode
	awsConfig := map[string]string{
		TLSSecretManagerKeySecretName:    "aws-secret",
		TLSSecretManagerKeySecretManager: AWSSecretManagerType,
		TLSSecretManagerKeyAWSRegion:     "us-east-1",
		TLSSecretManagerKeyTLSMode:       string(tlsModeEnable),
		TLSSecretManagerKeyKeyDataKey:    "key-data",
		TLSSecretManagerKeyCertDataKey:   "cert-data",
	}
	err = validateTLSConfigurationMap(awsConfig, "server", logger)
	assert.Nil(t, err)

	// Negative: missing AWS region
	awsConfigMissingRegion := cloneMap(awsConfig)
	delete(awsConfigMissingRegion, TLSSecretManagerKeyAWSRegion)
	err = validateTLSConfigurationMap(awsConfigMissingRegion, "server", logger)
	assert.ErrorContains(t, err, fmt.Sprintf("when the secret manager is %s, the %s key is required",
		AWSSecretManagerType, TLSSecretManagerKeyAWSRegion))

	// Negative: unknown tls mode
	configBadMode := cloneMap(validConfig)
	configBadMode[TLSSecretManagerKeyTLSMode] = "badmode"
	err = validateTLSConfigurationMap(configBadMode, "https", logger)
	assert.ErrorContains(t, err, fmt.Sprintf("the %s key's value must be one of %s",
		TLSSecretManagerKeyTLSMode, ValidTLSMode))

	// Negative: tls mode "disable" used for https config
	configDisableTLS := cloneMap(validConfig)
	configDisableTLS[TLSSecretManagerKeyTLSMode] = string(tlsModeDisable)
	err = validateTLSConfigurationMap(configDisableTLS, "https", logger)
	assert.ErrorContains(t, err, fmt.Sprintf("tls mode cannot be %s for https tls config", tlsModeDisable))

	// Negative: missing required key for verify-full
	configMissingCert := cloneMap(validConfig)
	delete(configMissingCert, TLSSecretManagerKeyCertDataKey)
	err = validateTLSConfigurationMap(configMissingCert, "https", logger)
	tlsMode := configMissingCert[TLSSecretManagerKeyTLSMode]
	assert.ErrorContains(t, err, fmt.Sprintf("when tls mode is %s, the %s key must exist",
		tlsMode, TLSSecretManagerKeyCertDataKey))
}

func cloneMap(orig map[string]string) map[string]string {
	copyMap := make(map[string]string)
	for k, v := range orig {
		copyMap[k] = v
	}
	return copyMap
}
