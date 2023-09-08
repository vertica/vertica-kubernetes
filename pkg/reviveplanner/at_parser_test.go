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

package reviveplanner

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("at_parser", func() {
	It("should parse the sample output", func() {
		parser := ATParser{}
		sampleOutput := `Attempting to retrieve file: [/db/cad47f8e-7cca-48dd-8d9c-a2403f6c457f/metadata/vertdb/cluster_config.json]

		Validated 1-node database vertdb defined at communal storage /db/cad47f8e-7cca-48dd-8d9c-a2403f6c457f.
		
		Expected layout of database after reviving from communal storage: /db/cad47f8e-7cca-48dd-8d9c-a2403f6c457f
		
		== Communal location details: ==
		{
		 "communal_storage_url": "/db/cad47f8e-7cca-48dd-8d9c-a2403f6c457f",
		 "num_shards": "6",
		 "depot_path": "/depot",
		 "depot_size": "287447467K"
		}
		
		Cluster lease expiration: 2023-02-01 15:07:32.022759
		
		== Database and node details: ==
		{
		 "nodes": [
		  {
		   "name": "v_vertdb_node0001",
		   "oid": 45035996273704986,
		   "catalogpath": "/data/vertdb/v_vertdb_node0001_catalog",
		   "storagelocs": [
			"/db/cad47f8e-7cca-48dd-8d9c-a2403f6c457f",
			"/data/vertdb/v_vertdb_node0001_data",
			"/depot/vertdb/v_vertdb_node0001_depot"
		   ],
		   "_vstorage_locations": [
			{
			 "name": "__location_0_v_vertdb_node0001",
			 "label": "",
			 "oid": 45035996273705020,
			 "path": "/data/vertdb/v_vertdb_node0001_data",
			 "sharing_type": 0,
			 "usage": 3,
			 "size": 0,
			 "site": 45035996273704986
			},
			{
			 "name": "__location_1_v_vertdb_node0001",
			 "label": "auto-data-depot",
			 "oid": 45035996273705156,
			 "path": "/depot/vertdb/v_vertdb_node0001_depot",
			 "sharing_type": 0,
			 "usage": 5,
			 "size": 294346206208,
			 "site": 45035996273704986
			}
		   ],
		   "_communal_storage_location": {
			"name": "__location_0_communal",
			"label": "",
			"oid": 45035996273705022,
			"path": "/db/cad47f8e-7cca-48dd-8d9c-a2403f6c457f",
			"sharing_type": 2,
			"usage": 1,
			"size": 0,
			"site": 0
		   },
		   "host": "10.244.0.47",
		   "port": 5433,
		   "controlnode": 45035996273704986,
		   "startcmd": null,
		   "isprimary": true
		  }
		 ],
		 "flags": {},
		 "name": "vertdb",
		 "version": 0,
		 "spreadversion": 0,
		 "controlmode": "pt2pt",
		 "deps": [],
		 "willupgrade": false,
		 "spreadEncryption": null,
		 "spreadEncryptionInUse": false
		}
		
		== Storage locations: ==
		[
		 {
		  "name": "__location_0_v_vertdb_node0001",
		  "label": "",
		  "oid": 45035996273705020,
		  "path": "/data/vertdb/v_vertdb_node0001_data",
		  "sharing_type": 0,
		  "usage": 3,
		  "size": 0,
		  "site": 45035996273704986
		 },
		 {
		  "name": "__location_0_communal",
		  "label": "",
		  "oid": 45035996273705022,
		  "path": "/db/cad47f8e-7cca-48dd-8d9c-a2403f6c457f",
		  "sharing_type": 2,
		  "usage": 1,
		  "size": 0,
		  "site": 0
		 },
		 {
		  "name": "__location_1_v_vertdb_node0001",
		  "label": "auto-data-depot",
		  "oid": 45035996273705156,
		  "path": "/depot/vertdb/v_vertdb_node0001_depot",
		  "sharing_type": 0,
		  "usage": 5,
		  "size": 294346206208,
		  "site": 45035996273704986
		 }
		]
		
		Number of primary nodes: 1`
		Expect(parser.Parse(sampleOutput)).Should(Succeed())
		Expect(parser.Database.Name).Should(Equal("vertdb"))
		Expect(len(parser.Database.Nodes)).Should(Equal(1))
		Expect(parser.Database.Nodes[0].Host).Should(Equal("10.244.0.47"))
		Expect(parser.Database.Nodes[0].Port).Should(Equal(5433))
		Expect(parser.CommunalLocation.NumShards).Should(Equal("6"))
		Expect(parser.CommunalLocation.DepotPath).Should(Equal("/depot"))
	})
})
