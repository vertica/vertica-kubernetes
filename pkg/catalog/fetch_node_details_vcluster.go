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
	"strconv"

	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodedetails"
)

// FetchNodeDetails returns details for a node, including its state, shard subscriptions, and depot details
func (v *VCluster) FetchNodeDetails(ctx context.Context) (nodeDetails *NodeDetails, err error) {
	vclusterOps := vadmin.MakeVClusterOps(v.Log, v.VDB, v.Client, v.Password, v.EVRec, vadmin.SetupVClusterOps)
	opts := []fetchnodedetails.Option{
		fetchnodedetails.WithInitiator(v.PodIP),
	}
	vnodeDetails, err := vclusterOps.FetchNodeDetails(ctx, opts...)
	if err != nil {
		return nil, err
	}
	nodeDetails = &NodeDetails{}
	nodeDetails.parseVNodeDetails(&vnodeDetails)
	return nodeDetails, nil
}

// parseVNodeDetails will parse node details returned by vclusterOps API
func (nodeDetails *NodeDetails) parseVNodeDetails(vnodeDetails *vclusterops.NodeDetails) {
	nodeDetails.Name = vnodeDetails.Name
	nodeDetails.State = vnodeDetails.State
	nodeDetails.SubclusterOid = strconv.FormatUint(vnodeDetails.SubclusterID, 10)
	nodeDetails.ReadOnly = vnodeDetails.IsReadOnly
	nodeDetails.SandboxName = vnodeDetails.SandboxName
	nodeDetails.ShardSubscriptions = int(vnodeDetails.NumberShardSubscriptions)
	if nodeDetails.ShardSubscriptions > 0 {
		nodeDetails.ShardSubscriptions--
	}
	for _, storageLoc := range vnodeDetails.StorageLocList {
		if storageLoc.UsageType == "DEPOT" {
			nodeDetails.MaxDepotSize = int(storageLoc.MaxSize)
			nodeDetails.DepotDiskPercentSize = storageLoc.DiskPercent
			break
		}
	}
}
