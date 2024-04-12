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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vcluster/vclusterops"
)

var _ = Describe("nodedetailsvcluster", func() {
	It("should parse node details from vclusterOps API correctly", func() {
		vnodeDetails := vclusterops.NodeDetails{
			NodeState: vclusterops.NodeState{
				Name:                     "v_test_db_node0001",
				ID:                       45035996273704992,
				Address:                  "192.168.1.101",
				State:                    "UP",
				Database:                 "test_db",
				IsPrimary:                false,
				IsReadOnly:               false,
				CatalogPath:              "/data/test_db/v_test_db_node0001_catalog/Catalog",
				DataPath:                 []string{"/data/test_db/v_test_db_node0001_data"},
				DepotPath:                "/data/test_db/v_test_db_node0001_depot",
				SubclusterName:           "sc1",
				SubclusterID:             45035996273704988,
				LastMsgFromNodeAt:        "2024-04-05T15:23:32.016281-04",
				DownSince:                "",
				Version:                  "v24.3.0-a0efe9ba3abb08d9e6472ffc29c8e0949b5998d2",
				SandboxName:              "sandbox1",
				NumberShardSubscriptions: 3,
			},
			StorageLocations: vclusterops.StorageLocations{
				StorageLocList: []vclusterops.StorageLocation{
					{
						Name:        "__location_0_v_test_db_node0001",
						ID:          45035996273705024,
						Label:       "",
						UsageType:   "DATA,TEMP",
						Path:        "/data/test_db/v_test_db_node0001_data",
						SharingType: "NONE",
						MaxSize:     0,
						DiskPercent: "",
						HasCatalog:  false,
						Retired:     false,
					},
					{
						Name:        "__location_1_v_test_db_node0001",
						ID:          45035996273705166,
						Label:       "auto-data-depot",
						UsageType:   "DEPOT",
						Path:        "/data/test_db/v_test_db_node0001_depot",
						SharingType: "NONE",
						MaxSize:     8215897325568,
						DiskPercent: "60%",
						HasCatalog:  false,
						Retired:     false,
					},
				},
			},
		}
		nodeDetails := &NodeDetails{}
		nodeDetails.parseVNodeDetails(&vnodeDetails)
		Expect(nodeDetails.Name).Should(Equal("v_test_db_node0001"))
		Expect(nodeDetails.State).Should(Equal("UP"))
		Expect(nodeDetails.SubclusterOid).Should(Equal("45035996273704988"))
		Expect(nodeDetails.ReadOnly).Should(BeFalse())
		Expect(nodeDetails.SandboxName).Should(Equal("sandbox1"))
		Expect(nodeDetails.ShardSubscriptions).Should(Equal(3))
		Expect(nodeDetails.MaxDepotSize).Should(Equal(8215897325568))
		Expect(nodeDetails.DepotDiskPercentSize).Should(Equal("60%"))
	})
})
