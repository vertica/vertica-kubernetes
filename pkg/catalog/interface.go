/*
 (c) Copyright [2021-2024] Open Text.
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

package catalog

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"k8s.io/apimachinery/pkg/types"
)

type Fetcher interface {
	// FetchNodeState will return information about a specific node
	FetchNodeState(ctx context.Context) (*NodeInfo, error)
}

type VSQL struct {
	PRunner           cmds.PodRunner
	VDB               *vapi.VerticaDB
	PodName           types.NamespacedName
	ExecContainerName string
}

// MakeVSQL will create a nodeInfoFetcher that uses vsql to get a node state
func MakeVSQL(vdb *vapi.VerticaDB, prunner cmds.PodRunner, pn types.NamespacedName, cnt string) *VSQL {
	return &VSQL{
		PRunner:           prunner,
		VDB:               vdb,
		PodName:           pn,
		ExecContainerName: cnt,
	}
}

type NodeInfo struct {
	Name          string
	State         string
	SubclusterOid string
	ReadOnly      bool
	SandboxName   string
}
