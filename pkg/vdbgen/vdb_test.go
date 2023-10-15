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

package vdbgen

import (
	"context"
	"database/sql"

	"github.com/DATA-DOG/go-sqlmock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	db   *sql.DB
	mock sqlmock.Sqlmock
)

func createMock() {
	var err error
	db, mock, err = sqlmock.New(sqlmock.MonitorPingsOption(true))
	Expect(err).Should(Succeed())
}

func deleteMock() {
	db.Close()
}

var _ = Describe("vdb", func() {
	ctx := context.Background()

	It("should init vdb from options", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{
			DBName:             "mydb",
			VdbName:            "vertdb",
			Image:              "my-img:latest",
			IgnoreClusterLease: true,
			DepotVolume:        "EmptyDir",
		}}
		dbGen.setParmsFromOptions()
		Expect(string(dbGen.Objs.Vdb.Spec.InitPolicy)).Should(Equal(vapi.CommunalInitPolicyRevive))
		Expect(dbGen.Objs.Vdb.Spec.DBName).Should(Equal("mydb"))
		Expect(dbGen.Objs.Vdb.ObjectMeta.Name).Should(Equal("vertdb"))
		Expect(dbGen.Objs.Vdb.GetIgnoreClusterLease()).Should(BeTrue())
		Expect(dbGen.Objs.Vdb.Spec.Image).Should(Equal("my-img:latest"))
		Expect(dbGen.Objs.Vdb.Spec.Local.DepotVolume).Should(Equal(vapi.EmptyDir))
	})

	It("should call ping() when we connect", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}

		mock.ExpectPing()
		Expect(dbGen.connect(ctx)).Should(Succeed())
		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should set shard count from sql query", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		mock.ExpectQuery("SELECT COUNT.* FROM SHARDS .*").
			WillReturnRows(sqlmock.NewRows([]string{"1"}).FromCSVString("12"))

		Expect(dbGen.setShardCount(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.ShardCount).Should(Equal(12))
		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should get communal endpoint for s3 from show database", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}
		dbGen.Objs.Vdb.Spec.Communal.Path = "s3://"

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("AWSEndpoint", "minio:30312").
				AddRow("AWSEnableHttps", "0").
				AddRow("other", "value").
				AddRow("AWSAuth", "minio:minio123"))
		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setCommunalEndpointAWS(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Communal.Endpoint).Should(Equal("http://minio:30312"))
		Expect(dbGen.Objs.CredSecret.Data[cloud.CommunalAccessKeyName]).Should(Equal([]byte("minio")))
		Expect(dbGen.Objs.CredSecret.Data[cloud.CommunalSecretKeyName]).Should(Equal([]byte("minio123")))

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("AWSEndpoint", "192.168.0.1").
				AddRow("AWSEnableHttps", "1").
				AddRow("other", "value").
				AddRow("AWSAuth", "auth:secret"))
		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setCommunalEndpointAWS(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Communal.Endpoint).Should(Equal("https://192.168.0.1"))

		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should handle case where auth isn't present for s3 communal db", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}
		dbGen.Objs.Vdb.Spec.Communal.Path = "s3://my-bucket-ut"

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("AWSEndpoint", "s3.amazonaws.com").
				AddRow("AWSEnableHttps", "1"))
		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setCommunalEndpointAWS(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Communal.Endpoint).Should(Equal("https://s3.amazonaws.com"))
		_, ok := dbGen.Objs.CredSecret.Data[cloud.CommunalAccessKeyName]
		Expect(ok).Should(BeFalse())
		_, ok = dbGen.Objs.CredSecret.Data[cloud.CommunalSecretKeyName]
		Expect(ok).Should(BeFalse())

		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should get communal endpoint for GCS from show database", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}
		dbGen.Objs.Vdb.Spec.Communal.Path = "gs://"

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("GCSEndpoint", "google.apis.com").
				AddRow("GCSEnableHttps", "1").
				AddRow("GCSAuth", "auth:secret").
				AddRow("GCSRegion", "US-WEST2"))
		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setCommunalEndpointGCloud(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Communal.Endpoint).Should(Equal("https://google.apis.com"))
		Expect(dbGen.Objs.Vdb.Spec.Communal.Region).Should(Equal("US-WEST2"))

		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should read azure json string from database defaults", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}
		dbGen.Objs.Vdb.Spec.Communal.Path = "azb://p1"

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("AzureStorageCredentials",
					`[{"accountName": "devopsvertica","accountKey": "secretKey"}]`))
		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setCommunalEndpointAzure(ctx)).Should(Succeed())
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureAccountName]).Should(Equal([]byte("devopsvertica")))
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureAccountKey]).Should(Equal([]byte("secretKey")))

		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should read correct azure credentials when more than one is present", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}
		dbGen.Objs.Vdb.Spec.Communal.Path = "azb://p2"

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("AzureStorageCredentials",
					`[{"accountName": "ingestOnly", "accountKey": "secretKey"},`+
						`{"accountName": "devopsvertica","blobEndpoint": "custom.endpoint","sharedAccessSignature": "secretSig"}]`))
		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		// Fail because more than one credential is present and nothing in the options
		Expect(dbGen.setCommunalEndpointAzure(ctx)).ShouldNot(Succeed())

		// Fail if the account name isn't present
		dbGen.Opts.AzureAccountName = "NotThere"
		Expect(dbGen.setCommunalEndpointAzure(ctx)).ShouldNot(Succeed())

		dbGen.Opts.AzureAccountName = "devopsvertica"
		Expect(dbGen.setCommunalEndpointAzure(ctx)).Should(Succeed())
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureAccountName]).Should(Equal([]byte("devopsvertica")))
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureBlobEndpoint]).Should(Equal([]byte("custom.endpoint")))
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureAccountKey]).Should(Equal([]byte("")))
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureSharedAccessSignature]).Should(Equal([]byte("secretSig")))

		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should use azure endpoint config to find proper endpoint", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}
		dbGen.Objs.Vdb.Spec.Communal.Path = "azb://p3"
		dbGen.Opts.AzureAccountName = "myacc"

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("AzureStorageCredentials",
					`[{"accountName": "myacc","blobEndpoint": "azurite:10000","accountKey": "key"}]`).
				AddRow("AzureStorageEndpointConfig",
					`[{"accountName": "myacc","blobEndpoint": "azurite:10000","protocol": "http"}]`))
		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setCommunalEndpointAzure(ctx)).Should(Succeed())
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureAccountName]).Should(Equal([]byte("myacc")))
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureBlobEndpoint]).Should(Equal([]byte("http://azurite:10000")))
		Expect(dbGen.Objs.CredSecret.Data[cloud.AzureAccountKey]).Should(Equal([]byte("key")))

		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should calculate and set requestSize", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		const expSQL = "SELECT MAX.* FROM DISK_STORAGE .*"
		mock.ExpectQuery(expSQL).
			WillReturnRows(sqlmock.NewRows([]string{"max"}).
				AddRow("122222"))
		mock.ExpectQuery(expSQL).
			WillReturnRows(sqlmock.NewRows([]string{"max"}).
				AddRow("122222"))

		expRequestSize := resource.MustParse("244444Mi")
		Expect(dbGen.setRequestSize(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Local.RequestSize).Should(Equal(expRequestSize))
	})

	It("should find ksafety from corresponding meta-function", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}
		dbGen.setParmsFromOptions()

		mock.ExpectQuery(Queries[KSafetyQueryKey]).
			WillReturnRows(sqlmock.NewRows([]string{"get_design_ksafe"}).
				AddRow("0"))
		mock.ExpectQuery("SELECT COUNT.* FROM NODES").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).
				AddRow("2"))

		Expect(dbGen.setKSafety(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.IsKSafety0()).Should(BeTrue())
	})

	It("should always set ksafety to '1' when the fetched value >= 1", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		mock.ExpectQuery(Queries[KSafetyQueryKey]).
			WillReturnRows(sqlmock.NewRows([]string{"get_design_ksafe"}).
				AddRow("2"))
		mock.ExpectQuery("SELECT COUNT.* FROM NODES").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).
				AddRow("4"))

		Expect(dbGen.setKSafety(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.IsKSafety0()).Should(BeFalse())
	})

	It("should raise an error if ksafety is '0' and the number of nodes > 3", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		mock.ExpectQuery(Queries[KSafetyQueryKey]).
			WillReturnRows(sqlmock.NewRows([]string{"get_design_ksafe"}).
				AddRow("0"))
		mock.ExpectQuery("SELECT COUNT.* FROM NODES").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).
				AddRow("4"))

		Expect(dbGen.setKSafety(ctx)).ShouldNot(Succeed())
	})

	It("should fetch the server version and use it to pick an image from the docker repo", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}
		dbGen.setParmsFromOptions()

		mock.ExpectQuery(Queries[VersionQueryKey]).
			WillReturnRows(sqlmock.NewRows([]string{"version"}).
				AddRow("Vertica Analytic Database 12.0.2-20221006"))
		mock.ExpectQuery(Queries[VersionQueryKey]).
			WillReturnRows(sqlmock.NewRows([]string{"version"}).
				AddRow("Vertica Analytic Database 11.0.1-0"))

		Expect(dbGen.setImage(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Image).Should(Equal("vertica/vertica-k8s:12.0.2-0"))
		Expect(dbGen.setImage(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Image).Should(Equal("vertica/vertica-k8s:11.0.1-0"))
	})

	It("should set as image the one specified on the command line", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{
			Image: "my-img:latest",
		}}
		dbGen.setParmsFromOptions()

		mock.ExpectQuery(Queries[VersionQueryKey]).
			WillReturnRows(sqlmock.NewRows([]string{"version"}).
				AddRow("Vertica Analytic Database 11.0.1-0"))

		Expect(dbGen.setImage(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Image).Should(Equal("my-img:latest"))
	})

	It("should extract common prefix for data, catalog and depot path", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		mock.ExpectQuery(Queries[StorageLocationKey]).
			WithArgs("DATA,TEMP").
			WillReturnRows(sqlmock.NewRows([]string{"node_name", "location_path"}).
				AddRow("v_vertdb_node0001", "/data/vertdb/v_vertdb_node0001_data").
				AddRow("v_vertdb_node0002", "/data/vertdb/v_vertdb_node0002_data"))
		mock.ExpectQuery(Queries[StorageLocationKey]).
			WithArgs("DEPOT").
			WillReturnRows(sqlmock.NewRows([]string{"node_name", "location_path"}).
				AddRow("v_vertdb_node0001", "/home/dbadmin/depot/vertdb/v_vertdb_node0001_data").
				AddRow("v_vertdb_node0002", "/home/dbadmin/depot/vertdb/v_vertdb_node0002_data"))
		mock.ExpectQuery(Queries[DiskStorageLocationKey]).
			WithArgs("CATALOG").
			WillReturnRows(sqlmock.NewRows([]string{"node_name", "location_path"}).
				AddRow("v_vertdb_node0001", "/catalog/vertdb/v_vertdb_node0001_catalog/Catalog").
				AddRow("v_vertdb_node0002", "/catalog/vertdb/v_vertdb_node0002_catalog/Catalog"))

		Expect(dbGen.setLocalPaths(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Local.DataPath).Should(Equal("/data"))
		Expect(dbGen.Objs.Vdb.Spec.Local.DepotPath).Should(Equal("/home/dbadmin/depot"))
		Expect(dbGen.Objs.Vdb.Spec.Local.CatalogPath).Should(Equal("/catalog"))

		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should raise an error if the local paths are different on two nodes", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		mock.ExpectQuery(Queries[StorageLocationKey]).
			WithArgs("DATA,TEMP").
			WillReturnRows(sqlmock.NewRows([]string{"node_name", "location_path"}).
				AddRow("v_vertdb_node0001", "/data1/vertdb/v_vertdb_node0001_data").
				AddRow("v_vertdb_node0002", "/data2/vertdb/v_vertdb_node0002_data"))

		Expect(dbGen.setLocalPaths(ctx)).ShouldNot(Succeed())
		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should find subcluster detail", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		const Sc1Name = "sc1"
		const Sc2Name = "sc2"
		const Sc3Name = "sc3"

		mock.ExpectQuery(Queries[SubclusterQueryKey]).
			WillReturnRows(sqlmock.NewRows([]string{"node_name", "location_path"}).
				AddRow(Sc1Name, true).AddRow(Sc1Name, true).AddRow(Sc1Name, true).
				AddRow(Sc2Name, false).AddRow(Sc1Name, true).AddRow(Sc3Name, true).
				AddRow(Sc3Name, true))

		expScDetail := []vapi.Subcluster{
			{Name: Sc1Name, Size: 4, IsPrimary: true},
			{Name: Sc2Name, Size: 1, IsPrimary: false},
			{Name: Sc3Name, Size: 2, IsPrimary: true},
		}
		expReviveOrder := []vapi.SubclusterPodCount{
			{SubclusterIndex: 0, PodCount: 3},
			{SubclusterIndex: 1, PodCount: 1},
			{SubclusterIndex: 0, PodCount: 1},
			{SubclusterIndex: 2, PodCount: 2},
		}

		Expect(dbGen.setSubclusterDetail(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Subclusters).Should(Equal(expScDetail))
		Expect(dbGen.Objs.Vdb.Spec.ReviveOrder).Should(Equal(expReviveOrder))
		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should fail if subcluster name is not suitable for Kubernetes", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		for _, name := range []string{"scCapital", "sc!bang", "sc-"} {
			mock.ExpectQuery(Queries[SubclusterQueryKey]).
				WillReturnRows(sqlmock.NewRows([]string{"node_name", "location_path"}).AddRow(name, true))

			Expect(dbGen.setSubclusterDetail(ctx)).ShouldNot(Succeed(), name)
			Expect(mock.ExpectationsWereMet()).Should(Succeed())
		}
	})

	It("should find communal path from storage location", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db}

		const expCommunalPath = "s3://nimbusdb/db/mspilchen"
		mock.ExpectQuery(Queries[StorageLocationKey]).
			WithArgs("DATA").
			WillReturnRows(sqlmock.NewRows([]string{"node_name", "location_path"}).
				AddRow("", expCommunalPath))

		Expect(dbGen.setCommunalPath(ctx)).Should(Succeed())
		Expect(dbGen.Objs.Vdb.Spec.Communal.Path).Should(Equal(expCommunalPath))
		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should include license if license data is present", func() {
		dbGen := DBGenerator{LicenseData: []byte("test"), Opts: &Options{}}
		dbGen.setParmsFromOptions()
		Expect(dbGen.setLicense(ctx)).Should(Succeed())
		Expect(len(dbGen.Objs.LicenseSecret.Data)).ShouldNot(Equal(0))
		Expect(len(dbGen.Objs.Vdb.Spec.LicenseSecret)).ShouldNot(Equal(0))
		Expect(dbGen.Objs.Vdb.Spec.LicenseSecret).Should(Equal(dbGen.Objs.LicenseSecret.ObjectMeta.Name))
	})

	It("should fail if CA file isn't present but one is in the db cfg", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("AWSEndpoint", "minio:30312").
				AddRow("AWSEnableHttps", "1").
				AddRow("AWSCAFile", "/certs/ca.crt").
				AddRow("AWSAuth", "minio:minio123"))

		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setCAFile(ctx)).ShouldNot(Succeed())

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("SystemCABundlePath", "/certs/ca.crt"))
		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setCAFile(ctx)).ShouldNot(Succeed())

		// Now correct the error by providing a ca file in the opts.
		dbGen.Opts.CAFile = "ca.crt"
		Expect(dbGen.setCAFile(ctx)).Should(Succeed())
		Expect(dbGen.Objs.HasCAFile).Should(BeTrue())
		Expect(len(dbGen.Objs.Vdb.Spec.CertSecrets)).Should(Equal(1))
	})

	It("should fail if kerberos options exist but Kerberos data was not loaded in", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{}}

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("KerberosRealm", "VERTICA.COM").
				AddRow("KerberosServiceName", "vertica"))

		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setKrb5Secret(ctx)).ShouldNot(Succeed())
		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})

	It("should succeed if Kerberos options exist and Kerberos data was loaded in", func() {
		createMock()
		defer deleteMock()

		dbGen := DBGenerator{Conn: db, Opts: &Options{},
			Krb5ConfData: []byte("data1"), Krb5KeytabData: []byte("data2")}
		dbGen.setParmsFromOptions()

		mock.ExpectQuery(Queries[DBCfgKey]).
			WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
				AddRow("KerberosRealm", "VERTICA.COM").
				AddRow("KerberosServiceName", "vertica"))

		Expect(dbGen.fetchDatabaseConfig(ctx)).Should(Succeed())
		Expect(dbGen.setKrb5Secret(ctx)).Should(Succeed())
		Expect(dbGen.Objs.HasKerberosSecret).Should(BeTrue())
		Expect(mock.ExpectationsWereMet()).Should(Succeed())
	})
})
