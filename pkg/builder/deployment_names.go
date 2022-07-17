/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package builder

import "fmt"

// DeploymentNames gives context about the names used in deploying the operator
type DeploymentNames struct {
	ServiceAccountName string // Name of the service account to use for vertica pods
	PrefixName         string // The common prefix for all objects created when deploying the operator
}

func (d *DeploymentNames) getConfigMapName() string {
	return fmt.Sprintf("%s-manager-config", d.PrefixName)
}

// DefaultDeploymentNames generates a DeploymentNames based on the defaults.
// This is for test purposes.
func DefaultDeploymentNames() *DeploymentNames {
	return &DeploymentNames{
		ServiceAccountName: "verticadb-operator-controller-manager",
		PrefixName:         "verticadb-operator",
	}
}
