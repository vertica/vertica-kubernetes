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
	"errors"
	"fmt"

	"golang.org/x/exp/slices"
)

type VerticaTLSModeType string

// TLS modes
const (
	tlsModeDisable    VerticaTLSModeType = "disable"
	tlsModeEnable     VerticaTLSModeType = "enable"
	tlsModeVerifyCA   VerticaTLSModeType = "verify_ca"
	tlsModeTryVerify  VerticaTLSModeType = "try_verify"
	tlsModeVerifyFull VerticaTLSModeType = "verify_full"
)

// Below constants are the key name used to set Vertica TLS configuration from K8S operator
/* example: {"Namespace": "default", "SecretManager": "kubernetes", "SecretName":"Secret"}*/
const (
	TLSSecretManagerKeyNamespace          string = "Namespace"
	TLSSecretManagerKeySecretManager      string = "SecretManager"
	TLSSecretManagerKeySecretName         string = "SecretName"
	TLSSecretManagerKeyCACertDataKey      string = "CADataKey"
	TLSSecretManagerKeyCertDataKey        string = "CertDataKey"
	TLSSecretManagerKeyKeyDataKey         string = "KeyDataKey"
	TLSSecretManagerKeyTLSMode            string = "TLSMode"
	TLSSecretManagerKeyAWSRegion          string = "AWSRegion"
	TLSSecretManagerKeyAWSSecretVersionID string = "AWSVersion"
)

// secret manager types
const (
	K8sSecretManagerType string = "kubernetes"
	AWSSecretManagerType string = "AWS"
	GCPSecretManagerType string = "GCP"
)

// config types
const (
	serverTLSKeyPrefix string = "server"
	httpsTLSKeyPrefix  string = "https"
)

var validSecretManagerType = []string{K8sSecretManagerType, GCPSecretManagerType, AWSSecretManagerType}
var ValidTLSMode = []VerticaTLSModeType{tlsModeDisable, tlsModeEnable,
	tlsModeVerifyCA, tlsModeTryVerify, tlsModeVerifyFull}

// The secret manager names
const (
	kubernetesSecretManagerName string = "KubernetesSecretManager"
	awsSecretManagerName        string = "AWSSecretManager"
)

// validateAllwaysRequiredKeys validates tls keys that must always be set in a
// tls configuration map
func validateAllwaysRequiredKeys(configMap map[string]string) error {
	if secretName, exist := configMap[TLSSecretManagerKeySecretName]; !exist || secretName == "" {
		return fmt.Errorf("the %s key must exist with a non-empty value", TLSSecretManagerKeySecretName)
	}
	if !slices.Contains(validSecretManagerType, configMap[TLSSecretManagerKeySecretManager]) {
		return fmt.Errorf("the %s key must exist and its value must be one of %s",
			TLSSecretManagerKeySecretManager, validSecretManagerType)
	}
	return nil
}

// validateRequiredKeysBasedOnSecretManager validates required tls keys based on the
// the secret manager that is passed
func validateRequiredKeysBasedOnSecretManager(configMap map[string]string) error {
	secretManager := configMap[TLSSecretManagerKeySecretManager]
	switch secretManager {
	case K8sSecretManagerType:
		if secretNamespace, exist := configMap[TLSSecretManagerKeyNamespace]; !exist || secretNamespace == "" {
			return fmt.Errorf("when the secret manager is %s, the %s key is required and must have a non-empty value",
				K8sSecretManagerType, TLSSecretManagerKeyNamespace)
		}
	case AWSSecretManagerType:
		if region, exist := configMap[TLSSecretManagerKeyAWSRegion]; !exist || region == "" {
			return fmt.Errorf("when the secret manager is %s, the %s key is required and must have a non-empty value",
				AWSSecretManagerType, TLSSecretManagerKeyAWSRegion)
		}
	case GCPSecretManagerType:
		return errors.New("not implemented")
	}
	return nil
}

// validateRequiredKeysBasedOnTLSMode validate required tls keys based on the given tls mode
func validateRequiredKeysBasedOnTLSMode(configMap map[string]string, configType string) error {
	tlsMode := configMap[TLSSecretManagerKeyTLSMode]
	if !slices.Contains(ValidTLSMode, VerticaTLSModeType(tlsMode)) {
		return fmt.Errorf("the %s key's value must be one of %s",
			TLSSecretManagerKeyTLSMode, ValidTLSMode)
	}
	if configType == httpsTLSKeyPrefix {
		if VerticaTLSModeType(tlsMode) == tlsModeDisable {
			return fmt.Errorf("tls mode cannot be %s for %s tls config", tlsModeDisable, configType)
		}
	}
	requiredKeys := getRequiredTLSConfigKeys(VerticaTLSModeType(tlsMode))
	for _, key := range requiredKeys {
		if _, exist := configMap[key]; !exist {
			return fmt.Errorf("when tls mode is %s, the %s key must exist and have a non-empty value",
				tlsMode, key)
		}
	}
	return nil
}

// getSecretManager given the secret manager type, returns
// the secret manager name
func getSecretManager(secretManagerType string) string {
	secretManagerMap := map[string]string{
		K8sSecretManagerType: kubernetesSecretManagerName,
		AWSSecretManagerType: awsSecretManagerName,
	}
	return secretManagerMap[secretManagerType]
}

// getRequiredTLSConfigKeys will return a list of required key names based on the TLS mode
func getRequiredTLSConfigKeys(tlsmode VerticaTLSModeType) []string {
	switch tlsmode {
	case tlsModeVerifyCA, tlsModeTryVerify, tlsModeVerifyFull:
		return []string{TLSSecretManagerKeyKeyDataKey, TLSSecretManagerKeyCACertDataKey, TLSSecretManagerKeyCertDataKey}
	case tlsModeEnable:
		return []string{TLSSecretManagerKeyKeyDataKey, TLSSecretManagerKeyCertDataKey}
	case tlsModeDisable:
		return []string{}
	default:
		return nil
	}
}
