/*
 (c) Copyright [2021-2023] Open Text.
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

package cloud

import (
	"strings"
)

type sourceType int

const (
	secretSourceK8s sourceType = iota
	secretSourceGSM
	secretSourceAWSSM
	defaultSecretSource = secretSourceK8s

	gsmPrefix   = "gsm"   // Google Secret Manager
	awssmPrefix = "awssm" // AWS Secrets Manager
)

// Maps a given prefix to the SourceType
var prefixToPathReference = map[string]sourceType{
	gsmPrefix:   secretSourceGSM,
	awssmPrefix: secretSourceAWSSM,
}

// getSecretSourceType returns the source of a secret given its name
func getSecretSourceType(secretName string) (stype sourceType, nameWithoutPathReference string) {
	comps := strings.Split(secretName, "://")
	if len(comps) <= 1 {
		return defaultSecretSource, secretName
	}
	source, ok := prefixToPathReference[comps[0]]
	if !ok {
		return defaultSecretSource, secretName
	}
	return source, comps[1]
}

// IsGSMSecret returns true if the given secret name should be fetched from
// Google Secret Manager (GSM)
func IsGSMSecret(secretName string) bool {
	stype, _ := getSecretSourceType(secretName)
	return stype == secretSourceGSM
}

// IsAWSSecretsManagerSecret returns true if the given secret name is stored in
// AWS Secrets Manager.
func IsAWSSecretsManagerSecret(secretName string) bool {
	stype, _ := getSecretSourceType(secretName)
	return stype == secretSourceAWSSM
}

// IsK8sSecret returns true if the given secret should be fetched directly from
// Kubernetes secret manager.
func IsK8sSecret(secretName string) bool {
	stype, _ := getSecretSourceType(secretName)
	return stype == secretSourceK8s
}

// RemovePathReference returns the name of the secret without the path reference
func RemovePathReference(secretName string) string {
	_, nameWithoutPathReference := getSecretSourceType(secretName)
	return nameWithoutPathReference
}
