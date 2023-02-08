/*
 (c) Copyright [2021-2023] Micro Focus or one of its affiliates.
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

package reviveplanner

import (
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

type Planner interface {
	// Analyze will look at the given output, from revive --display-only, and
	// parse it into Go structs.
	Parse(op string) error

	// IsCompatible will check if the revive will even work in k8s. A failure
	// message is returned if it isn't compatible.
	IsCompatible() (string, bool)

	// ApplyChanges will update the input vdb based on things it found during
	// analysis. Return true if the vdb was updated.
	ApplyChanges(vdb *vapi.VerticaDB) (bool, error)
}
