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
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

var _ = Describe("analyze", func() {
	It("should be able to extract out a common prefix", func() {
		p := ATPlanner{
			Database: Database{
				Name: "vertdb",
			},
		}
		Expect(p.extractPathPrefix("/data/vertdb/v_vertdb_node0001_catalog")).Should(Equal("/data"))
		Expect(p.extractPathPrefix("/one/path/vertdb/v_vertdb_node0101_data")).Should(Equal("/one/path"))
		_, err := p.extractPathPrefix("/data/not-valid")
		Expect(err).ShouldNot(Succeed())
	})

	It("should be able to extract out a common prefix if db has capital letters", func() {
		p := ATPlanner{
			Database: Database{
				Name: "Vertica_Dashboard",
			},
		}
		Expect(p.extractPathPrefix("/vertica/dat/Vertica_Dashboard/v_vertica_dashboard_node0001_data")).Should(Equal("/vertica/dat"))
	})

	It("should be able to find common paths", func() {
		p := ATPlanner{
			Database: Database{
				Name: "v",
			},
		}
		Expect(p.getCommonPath([]string{
			"/data/prefix/v/v_v_node0001_depot",
			"/data/prefix/v/v_v_node0002_depot",
			"/data/prefix/v/v_v_node0003_depot",
		}, "")).Should(Equal("/data/prefix"))
		_, err := p.getCommonPath([]string{
			"/p1/v/v_v_node0001_depot",
			"/p2/v/v_v_node0002_depot",
		}, "")
		Expect(err).ShouldNot(Succeed())
		_, err = p.getCommonPath(nil, "")
		Expect(err).ShouldNot(Succeed())
		_, err = p.getCommonPath([]string{
			"/p1/v/v_v_node0001_depot",
			"/p1/v/invalid/path/no/vnode",
		}, "")
		Expect(err).ShouldNot(Succeed())
	})

	It("should be able to find common paths after accounting for an outlier", func() {
		p := ATPlanner{
			Database: Database{
				Name: "v",
			},
		}
		Expect(p.getCommonPath([]string{
			"/path1/prefix/v/v_v_node0001_data",
			"/outlier/prefix/v/v_v_node0002_data",
			"/path1/prefix/v/v_v_node0003_data",
		}, "/outlier/prefix")).Should(Equal("/path1/prefix"))
		_, err := p.getCommonPath([]string{
			"/p1/v/v_v_node0001_data",
			"/p2/v/v_v_node0002_data",
		}, "/outlier")
		Expect(err).ShouldNot(Succeed())
		Expect(p.getCommonPath([]string{
			"/p1/v/v_v_node0001_data",
			"/p1/v/v_v_node0002_data",
		}, "/p1")).Should(Equal("/p1"))
	})

	It("should update vdb based on revive output", func() {
		vdb := vapi.MakeVDB()
		p := MakeATPlannerFromVDB(vdb, logger)

		origVdb := vdb.DeepCopy()

		// Change some things in vdb that the planner will change back
		vdb.Spec.ShardCount = 50
		vdb.Spec.Local.DataPath = "/new-data/location/is/here"
		vdb.Spec.Local.CatalogPath = "/somewhere"
		vdb.Spec.Local.DepotPath = "/depot-location_is_here/subdir"

		Expect(p.ApplyChanges(vdb)).Should(BeTrue())
		Expect(vdb.Spec.ShardCount).Should(Equal(origVdb.Spec.ShardCount))
		Expect(vdb.Spec.Local.DataPath).Should(Equal(origVdb.Spec.Local.DataPath))
		Expect(vdb.Spec.Local.GetCatalogPath()).Should(Equal(origVdb.Spec.Local.GetCatalogPath()))
		Expect(vdb.Spec.Local.DepotPath).Should(Equal(origVdb.Spec.Local.DepotPath))
	})

	It("should say revive isn't compatible if paths differ among nodes", func() {
		p := ATPlanner{
			Database: Database{
				Name: "mydb",
				Nodes: []Node{
					{
						Name:        "v_mydb_node0001",
						CatalogPath: "/cat/mydb/v_mydb_node0001_catalog",
						VStorageLocations: []StorageLocation{
							{
								Path:  "/dep/mydb/v_mydb_node0001_depot",
								Usage: UsageIsDepot,
							},
							{
								Path:  "/dat/mydb/v_mydb_node0001_data",
								Usage: UsageIsDataTemp,
							},
						},
					}, {
						Name:        "v_mydb_node0002",
						CatalogPath: "/cat/mydb/v_mydb_node0002_catalog",
						VStorageLocations: []StorageLocation{
							{
								Path:  "/dep/mydb/v_mydb_node0002_depot",
								Usage: UsageIsDepot,
							},
							{
								Path:  "/dat/mydb/v_mydb_node0002_data",
								Usage: UsageIsDataTemp,
							},
						},
					},
				},
			},
		}
		_, ok := p.IsCompatible()
		Expect(ok).Should(BeTrue())

		origCatPath := p.Database.Nodes[1].CatalogPath
		p.Database.Nodes[1].CatalogPath = fmt.Sprintf("/something-not-common%s", origCatPath)
		msg, ok := p.IsCompatible()
		Expect(ok).Should(BeFalse())
		Expect(len(msg)).ShouldNot(Equal(0))
		p.Database.Nodes[1].CatalogPath = origCatPath

		origDepotPath := p.Database.Nodes[1].VStorageLocations[0].Path
		p.Database.Nodes[1].VStorageLocations[0].Path = fmt.Sprintf("/a%s", origDepotPath)
		msg, ok = p.IsCompatible()
		Expect(ok).Should(BeFalse())
		Expect(len(msg)).ShouldNot(Equal(0))
		p.Database.Nodes[1].VStorageLocations[0].Path = origDepotPath

		origDataPath := p.Database.Nodes[0].VStorageLocations[1].Path
		p.Database.Nodes[0].VStorageLocations[1].Path = fmt.Sprintf("/b%s", origDataPath)
		msg, ok = p.IsCompatible()
		Expect(ok).Should(BeFalse())
		Expect(len(msg)).ShouldNot(Equal(0))
		p.Database.Nodes[0].VStorageLocations[1].Path = origDataPath

		_, ok = p.IsCompatible()
		Expect(ok).Should(BeTrue())
	})
})
