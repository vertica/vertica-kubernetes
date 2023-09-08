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

var _ = Describe("vc_parser", func() {
	It("should parse the sample output from vcluster --display-only", func() {
		parser := VCParser{}
		sampleOutput := `
{
   "CatalogTruncationVersion" : 55,
   "ClusterLeaseExpiration" : "2023-09-07 18:10:35.015395",
   "Database" : {
      "branch" : "",
      "name" : "vertdb"
   },
   "DatabaseVersion" : "v23.4.0-20230905",
   "GlobalSettings" : {
      "TupleMoverServices" : -33,
      "appliedUpgrades" : [
         "APLicenseUpgrade",
         "AddBlobResourcePool",
         "AddInternodeTLSConfig",
         "AddLocalProjection",
         "UserProfiles"
      ],
      "controlAddrFamily" : "ipv4",
      "controlConfExtras" : "ExitOnIdle = yes",
      "controlDebugFlags" : "PRINT EXIT",
      "controlLog" : "/dev/null",
      "controlMode" : "pt2pt",
      "controlTimeout" : 0,
      "controlType" : "spread",
      "currentCatalogRevision" : "4ecb4921d3614ab74baed21e23a3bb0597ac3398",
      "currentCatalogVersion" : "v23.4.0-20230905",
      "currentClientBalancingPolicy" : "none",
      "dbCreateVersion" : "v23.4.0-20230905",
      "dcPolicy" : [],
      "numControlNodes" : 120,
      "oid" : 45035996273705030,
      "preferredKSafe" : 1,
      "recoverByTable" : true,
      "restartPolicy" : "ksafe",
      "sand" : false,
      "spreadSecurityDetails" : "",
      "tag" : 0
   },
   "GlobalVersion" : "55",
   "IncarnationID" : "d887bbbb374c63e7d3c9ca697710bd",
   "Node" : [
      {
         "address" : "10.244.0.7",
         "addressFamily" : "ipv4",
         "catalogPath" : "/data/vertdb/v_vertdb_node0001_catalog/Catalog",
         "clientPort" : 5433,
         "controlAddress" : "10.244.0.7",
         "controlAddressFamily" : "ipv4",
         "controlBroadcast" : "10.244.0.255",
         "controlNode" : 45035996273704986,
         "controlPort" : 4803,
         "ei_address" : 0,
         "hasCatalog" : false,
         "isEphemeral" : false,
         "isPrimary" : true,
         "isRecoveryClerk" : true,
         "name" : "v_vertdb_node0001",
         "nodeParamMap" : [],
         "nodeType" : 0,
         "oid" : 45035996273704986,
         "parentFaultGroupId" : 45035996273704982,
         "replacedNode" : 0,
         "sand" : false,
         "schema" : 0,
         "siteUniqueID" : 10,
         "tag" : 0
      },
      {
         "address" : "10.244.0.5",
         "addressFamily" : "ipv4",
         "catalogPath" : "/data/vertdb/v_vertdb_node0002_catalog/Catalog",
         "clientPort" : 5433,
         "controlAddress" : "10.244.0.5",
         "controlAddressFamily" : "ipv4",
         "controlBroadcast" : "10.244.0.255",
         "controlNode" : 45035996273705158,
         "controlPort" : 4803,
         "ei_address" : 0,
         "hasCatalog" : false,
         "isEphemeral" : false,
         "isPrimary" : true,
         "isRecoveryClerk" : false,
         "name" : "v_vertdb_node0002",
         "nodeParamMap" : [],
         "nodeType" : 0,
         "oid" : 45035996273705158,
         "parentFaultGroupId" : 45035996273704982,
         "replacedNode" : 0,
         "sand" : false,
         "schema" : 0,
         "siteUniqueID" : 11,
         "tag" : 0
      },
      {
         "address" : "10.244.0.9",
         "addressFamily" : "ipv4",
         "catalogPath" : "/data/vertdb/v_vertdb_node0003_catalog/Catalog",
         "clientPort" : 5433,
         "controlAddress" : "10.244.0.9",
         "controlAddressFamily" : "ipv4",
         "controlBroadcast" : "10.244.0.255",
         "controlNode" : 45035996273705162,
         "controlPort" : 4803,
         "ei_address" : 0,
         "hasCatalog" : false,
         "isEphemeral" : false,
         "isPrimary" : true,
         "isRecoveryClerk" : false,
         "name" : "v_vertdb_node0003",
         "nodeParamMap" : [],
         "nodeType" : 0,
         "oid" : 45035996273705162,
         "parentFaultGroupId" : 45035996273704982,
         "replacedNode" : 0,
         "sand" : false,
         "schema" : 0,
         "siteUniqueID" : 12,
         "tag" : 0
      }
   ],
   "ShardCount" : 7,
   "SpreadEncryption" : "",
   "SpreadEncryptionAtRestart" : "",
   "SpreadVersion" : "44",
   "StartCommands" : {
      "v_vertdb_node0001" : "\"/opt/vertica/bin/vertica\" \"-D\" \"/data/vertdb/v_vertdb_node0001_catalog\" \"-C\" \"vertdb\" \"-n\" \"v_vertdb_node0001\" \"-h\" \"10.244.0.7\" \"-p\" \"5433\" \"-P\" \"4803\" \"-Y\" \"ipv4\"",
      "v_vertdb_node0002" : "\"/opt/vertica/bin/vertica\" \"-D\" \"/data/vertdb/v_vertdb_node0002_catalog\" \"-C\" \"vertdb\" \"-n\" \"v_vertdb_node0002\" \"-h\" \"10.244.0.5\" \"-p\" \"5433\" \"-P\" \"4803\" \"-Y\" \"ipv4\"",
      "v_vertdb_node0003" : "\"/opt/vertica/bin/vertica\" \"-D\" \"/data/vertdb/v_vertdb_node0003_catalog\" \"-C\" \"vertdb\" \"-n\" \"v_vertdb_node0003\" \"-h\" \"10.244.0.9\" \"-p\" \"5433\" \"-P\" \"4803\" \"-Y\" \"ipv4\""
   },
   "StorageLocation" : [
      {
         "diskPercent" : "",
         "fsOid" : 0,
         "hasCatalog" : false,
         "label" : "",
         "latency" : 0,
         "name" : "__location_0_v_vertdb_node0001",
         "oid" : 45035996273705026,
         "path" : "/data/vertdb/v_vertdb_node0001_data",
         "rank" : 1,
         "retired" : false,
         "sand" : false,
         "schema" : 0,
         "sharingType" : 0,
         "site" : 45035996273704986,
         "size" : 0,
         "tag" : 0,
         "throughput" : 0,
         "usage" : 3
      },
      {
         "diskPercent" : "",
         "fsOid" : 0,
         "hasCatalog" : true,
         "label" : "",
         "latency" : 0,
         "name" : "__location_0_communal",
         "oid" : 45035996273705028,
         "path" : "/communal/0ae3e837-3167-402f-a8f7-316ffdfd65f3",
         "rank" : 0,
         "retired" : false,
         "sand" : false,
         "schema" : 0,
         "sharingType" : 2,
         "site" : 0,
         "size" : 0,
         "tag" : 0,
         "throughput" : 0,
         "usage" : 1
      },
      {
         "diskPercent" : "",
         "fsOid" : 0,
         "hasCatalog" : false,
         "label" : "",
         "latency" : 0,
         "name" : "__location_0_v_vertdb_node0002",
         "oid" : 45035996273705160,
         "path" : "/data/vertdb/v_vertdb_node0002_data",
         "rank" : 1,
         "retired" : false,
         "sand" : false,
         "schema" : 0,
         "sharingType" : 0,
         "site" : 45035996273705158,
         "size" : 0,
         "tag" : 0,
         "throughput" : 0,
         "usage" : 3
      },
      {
         "diskPercent" : "",
         "fsOid" : 0,
         "hasCatalog" : false,
         "label" : "",
         "latency" : 0,
         "name" : "__location_0_v_vertdb_node0003",
         "oid" : 45035996273705164,
         "path" : "/data/vertdb/v_vertdb_node0003_data",
         "rank" : 1,
         "retired" : false,
         "sand" : false,
         "schema" : 0,
         "sharingType" : 0,
         "site" : 45035996273705162,
         "size" : 0,
         "tag" : 0,
         "throughput" : 0,
         "usage" : 3
      },
      {
         "diskPercent" : "60%",
         "fsOid" : 0,
         "hasCatalog" : false,
         "label" : "auto-data-depot",
         "latency" : 60,
         "name" : "__location_1_v_vertdb_node0001",
         "oid" : 45035996273705190,
         "path" : "/depot/vertdb/v_vertdb_node0001_depot",
         "rank" : 0,
         "retired" : false,
         "sand" : false,
         "schema" : 0,
         "sharingType" : 0,
         "site" : 45035996273704986,
         "size" : 1180042006528,
         "tag" : 0,
         "throughput" : 200,
         "usage" : 5
      },
      {
         "diskPercent" : "60%",
         "fsOid" : 0,
         "hasCatalog" : false,
         "label" : "auto-data-depot",
         "latency" : 60,
         "name" : "__location_1_v_vertdb_node0002",
         "oid" : 45035996273705192,
         "path" : "/depot/vertdb/v_vertdb_node0002_depot",
         "rank" : 0,
         "retired" : false,
         "sand" : false,
         "schema" : 0,
         "sharingType" : 0,
         "site" : 45035996273705158,
         "size" : 1180042006528,
         "tag" : 0,
         "throughput" : 200,
         "usage" : 5
      },
      {
         "diskPercent" : "60%",
         "fsOid" : 0,
         "hasCatalog" : false,
         "label" : "auto-data-depot",
         "latency" : 60,
         "name" : "__location_1_v_vertdb_node0003",
         "oid" : 45035996273705194,
         "path" : "/depot/vertdb/v_vertdb_node0003_depot",
         "rank" : 0,
         "retired" : false,
         "sand" : false,
         "schema" : 0,
         "sharingType" : 0,
         "site" : 45035996273705162,
         "size" : 1180042006528,
         "tag" : 0,
         "throughput" : 200,
         "usage" : 5
      }
   ],
   "SuperUser" : "dbadmin",
   "Timestamp" : "2023-09-07 17:55:35.015395"
}`
		Ω(parser.Parse(sampleOutput)).Should(Succeed())
		Ω(parser.getDatabaseName()).Should(Equal("vertdb"))
		Ω(parser.getNumShards()).Should(Equal(6))
		depotPaths := parser.getDepotPaths()
		Ω(depotPaths).Should(HaveLen(3))
		Ω(depotPaths).Should(ContainElements(
			"/depot/vertdb/v_vertdb_node0001_depot",
			"/depot/vertdb/v_vertdb_node0002_depot",
			"/depot/vertdb/v_vertdb_node0003_depot",
		))
		dataPaths := parser.getDataPaths()
		Ω(dataPaths).Should(HaveLen(3))
		Ω(dataPaths).Should(ContainElements(
			"/data/vertdb/v_vertdb_node0001_data",
			"/data/vertdb/v_vertdb_node0002_data",
			"/data/vertdb/v_vertdb_node0003_data",
		))
		catalogPaths := parser.getCatalogPaths()
		Ω(catalogPaths).Should(HaveLen(3))
		Ω(catalogPaths).Should(ContainElements(
			"/data/vertdb/v_vertdb_node0001_catalog",
			"/data/vertdb/v_vertdb_node0002_catalog",
			"/data/vertdb/v_vertdb_node0003_catalog",
		))
	})
})
