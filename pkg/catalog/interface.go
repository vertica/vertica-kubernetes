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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NodeDetails struct {
	Name                 string
	State                string
	SubclusterOid        string
	ReadOnly             bool
	SandboxName          string
	ShardSubscriptions   int
	MaxDepotSize         int
	DepotDiskPercentSize string
}

type Fetcher interface {
	// FetchNodeDetails will return information about a specific node
	FetchNodeDetails(ctx context.Context) (*NodeDetails, error)
}

type VSQL struct {
	PRunner           cmds.PodRunner
	VDB               *vapi.VerticaDB
	PodName           types.NamespacedName
	ExecContainerName string
	VNodeName         string
}

// MakeVSQL will create a Fetcher that uses vsql to get a node's details
func MakeVSQL(vdb *vapi.VerticaDB, prunner cmds.PodRunner, pn types.NamespacedName, cnt, vnodeName string) *VSQL {
	return &VSQL{
		PRunner:           prunner,
		VDB:               vdb,
		PodName:           pn,
		ExecContainerName: cnt,
		VNodeName:         vnodeName,
	}
}

type VCluster struct {
	VDB      *vapi.VerticaDB
	Password string
	PodIP    string
	Log      logr.Logger
	client.Client
	EVRec record.EventRecorder
}

// MakeVCluster will create a Fetcher that uses vclusterops API to get a node's details
func MakeVCluster(vdb *vapi.VerticaDB, password, podIP string, log logr.Logger,
	cli client.Client, evRec record.EventRecorder) *VCluster {
	return &VCluster{
		VDB:      vdb,
		Password: password,
		PodIP:    podIP,
		Log:      log,
		Client:   cli,
		EVRec:    evRec,
	}
}
