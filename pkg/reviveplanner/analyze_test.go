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

package reviveplanner

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner/atparser"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner/util"
)

var _ = Describe("analyze", func() {
	It("should be able to extract out a common prefix", func() {
		parser := atparser.Parser{
			Database: atparser.Database{
				Name: "vertdb",
			},
		}
		p := Planner{Parser: &parser}
		pathPrefix, ok := p.extractPathPrefixFromVNodePath("/data/vertdb/v_vertdb_node0001_catalog")
		Expect(ok).Should(BeTrue())
		Expect(pathPrefix).Should(Equal("/data"))
		pathPrefix, ok = p.extractPathPrefixFromVNodePath("/one/path/vertdb/v_vertdb_node0101_data")
		Expect(ok).Should(BeTrue())
		Expect(pathPrefix).Should(Equal("/one/path"))
		_, ok = p.extractPathPrefixFromVNodePath("/data/not-valid")
		Expect(ok).ShouldNot(BeTrue())
	})

	It("should be able to extract out a common prefix if db has capital letters", func() {
		parser := atparser.Parser{
			Database: atparser.Database{
				Name: "Vertica_Dashboard",
			},
		}
		p := Planner{Parser: &parser}
		pathPrefix, ok := p.extractPathPrefixFromVNodePath("/vertica/dat/Vertica_Dashboard/v_vertica_dashboard_node0001_data")
		Expect(ok).Should(BeTrue())
		Expect(pathPrefix).Should(Equal("/vertica/dat"))
	})

	It("should be able to find local paths", func() {
		parser := atparser.Parser{
			Database: atparser.Database{
				Name: "v",
			},
		}
		p := Planner{Parser: &parser}
		Expect(p.getLocalPaths([]string{
			"/data/prefix/v/v_v_node0001_depot",
			"/data/prefix/v/v_v_node0002_depot",
			"/data/prefix/v/v_v_node0003_depot",
		})).Should(Equal([]string{"/data/prefix"}))

		paths := []string{
			"/p1/v/v_v_node0001_depot",
			"/p2/v/v_v_node0002_depot",
		}
		// expected output
		paths2 := []string{
			"/p1",
			"/p2",
		}
		outPaths, err := p.getLocalPaths(paths)
		Expect(err).Should(BeNil())
		Expect(outPaths).To(ConsistOf(paths2))

		_, err = p.getLocalPaths(nil)
		Expect(err).ShouldNot(Succeed())

		paths = []string{
			"/p1/v/v_v_node0001_depot",
			"/p2/v/path/no/vnode",
		}
		// expected output
		paths2 = []string{
			"/p1",
			"/p2/v/path/no/vnode",
		}
		outPaths, err = p.getLocalPaths(paths)
		Expect(err).Should(BeNil())
		Expect(outPaths).To(ConsistOf(paths2))

		// ignore remote paths
		paths = append(paths, "s3://some/path")
		outPaths, err = p.getLocalPaths(paths)
		Expect(err).Should(BeNil())
		Expect(outPaths).To(ConsistOf(paths2))

		// a mixed user-created paths and vertica-created paths
		paths = []string{
			"",                                  // will be ignored since it's not an absolute path
			"some/path1",                        // will be ignored since it's not an absolute path
			"/some/path2/",                      // the trailing slash should be removed
			"//some///path3/",                   // the double/treble slashes should be changed to single slash
			"data/prefix/v/v_v_node0001_depot",  // ignored since it's not an absolute path
			"/data/prefix/v/v_v_node0001_depot", // should only use the prefix
			"/data/prefix/v/v_v_node0002_depot", // should only use the prefix
		}
		// expected output
		paths2 = []string{
			"/some/path2",
			"/some/path3",
			"/data/prefix",
		}
		outPaths, err = p.getLocalPaths(paths)
		Expect(err).Should(BeNil())
		Expect(outPaths).To(ConsistOf(paths2))
	})

	It("should update vdb based on revive output", func() {
		vdb := vapi.MakeVDB()
		parser := atparser.MakeATParserFromVDB(vdb, logger)
		p := Planner{Parser: &parser}

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

	It("should update depotVolume when is EmptyDir and depot path is not unique", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Local.DepotPath = vdb.Spec.Local.DataPath
		parser := atparser.MakeATParserFromVDB(vdb, logger)
		p := Planner{Parser: &parser}

		origVdb := vdb.DeepCopy()

		// Change some things in vdb that the planner will change back
		vdb.Spec.Local.DataPath = "/data-dir"
		vdb.Spec.Local.DepotVolume = vapi.EmptyDir

		Expect(p.ApplyChanges(vdb)).Should(BeTrue())
		Expect(vdb.Spec.Local.DataPath).Should(Equal(origVdb.Spec.Local.DataPath))
		Expect(vdb.Spec.Local.GetCatalogPath()).Should(Equal(origVdb.Spec.Local.GetCatalogPath()))
		Expect(vdb.Spec.Local.DepotPath).Should(Equal(origVdb.Spec.Local.DepotPath))
		Expect(vdb.Spec.Local.DepotVolume).Should(Equal(vapi.PersistentVolume))
	})

	It("should say revive isn't compatible if paths differ among nodes", func() {
		p := atparser.Parser{
			Database: atparser.Database{
				Name: "mydb",
				Nodes: []atparser.Node{
					{
						Name:        "v_mydb_node0001",
						CatalogPath: "/cat/mydb/v_mydb_node0001_catalog",
						VStorageLocations: []atparser.StorageLocation{
							{
								Path:  "/dep/mydb/v_mydb_node0001_depot",
								Usage: util.UsageIsDepot,
							},
							{
								Path:  "/dat/mydb/v_mydb_node0001_data",
								Usage: util.UsageIsDataTemp,
							},
						},
					}, {
						Name:        "v_mydb_node0002",
						CatalogPath: "/cat/mydb/v_mydb_node0002_catalog",
						VStorageLocations: []atparser.StorageLocation{
							{
								Path:  "/dep/mydb/v_mydb_node0002_depot",
								Usage: util.UsageIsDepot,
							},
							{
								Path:  "/dat/mydb/v_mydb_node0002_data",
								Usage: util.UsageIsDataTemp,
							},
						},
					},
				},
			},
		}
		plnr := Planner{Parser: &p}
		_, ok := plnr.IsCompatible()
		Expect(ok).Should(BeTrue())

		origCatPath := p.Database.Nodes[1].CatalogPath
		p.Database.Nodes[1].CatalogPath = fmt.Sprintf("/something-not-common%s", origCatPath)
		msg, ok := plnr.IsCompatible()
		Expect(ok).Should(BeFalse())
		Expect(len(msg)).ShouldNot(Equal(0))
		p.Database.Nodes[1].CatalogPath = origCatPath

		_, ok = plnr.IsCompatible()
		Expect(ok).Should(BeTrue())
	})
})
