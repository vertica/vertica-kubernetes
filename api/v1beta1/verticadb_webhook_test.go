/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package v1beta1

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("verticadb_webhook", func() {
	// validate VerticaDB spec values
	It("should succeed with all valid fields", func() {
		vdb := createVDBHelper()
		validateSpecValuesHaveErr(vdb, false)
	})
	It("should not have DB name more than 30 characters", func() {
		vdb := createVDBHelper()
		vdb.Spec.DBName = "VeryLongLongLongLongVerticaDBName"
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have invalid character in DB name", func() {
		vdb := createVDBHelper()
		vdb.Spec.DBName = "vertica-db"
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have invalid character in DB name", func() {
		vdb := createVDBHelper()
		vdb.Spec.DBName = "vertica+db"
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should have at least one primary subcluster", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.IsPrimary = false
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have 0 pod when kSafety is 0", func() {
		vdb := createVDBHelper()
		vdb.Spec.KSafety = KSafety0
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 0
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have more than 3 pods when kSafety is 0", func() {
		vdb := createVDBHelper()
		vdb.Spec.KSafety = KSafety0
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 5
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have less than 3 pods when kSafety is 1", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have invalid communal path", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Path = "http://nimbusdb/mspilchen"
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have invalid communal endpoint", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Endpoint = "s3://minio"
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should have invalid subcluster name", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.Name = "default-subcluster"
		validateSpecValuesHaveErr(vdb, false)
	})
	It("should not have invalid subcluster name", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.Name = "default_subcluster"
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have empty credentialsecret", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.CredentialSecret = ""
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have nodePort smaller than 30000", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.ServiceType = v1.ServiceTypeNodePort
		sc.NodePort = 5555
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have nodePort bigger than 32767", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.ServiceType = v1.ServiceTypeNodePort
		sc.NodePort = 55555
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have duplicate subcluster names", func() {
		const duplicateScName = "duplicatename"
		vdb := createVDBHelper()
		sc1 := &vdb.Spec.Subclusters[0]
		sc1.Name = duplicateScName
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, Subcluster{
			Name: duplicateScName,
			Size: 3,
		})
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should have at least one subcluster defined", func() {
		vdb := MakeVDB()
		vdb.Spec.Subclusters = []Subcluster{}
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have volume names conflicting with existing mount points", func() {
		vdb := createVDBHelper()
		vdb.Spec.Volumes = []v1.Volume{
			{
				Name: PodInfoMountName,
			},
			{
				Name: LocalDataPVC,
			},
			{
				Name: LicensingMountName,
			},
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Volumes = []v1.Volume{
			{
				Name: PodInfoMountName,
			},
			{
				Name: "validname",
			},
			{
				Name: LicensingMountName,
			},
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Volumes = []v1.Volume{
			{
				Name: "validname1",
			},
			{
				Name: "validname2",
			},
			{
				Name: LicensingMountName,
			},
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Volumes = []v1.Volume{
			{
				Name: "validname1",
			},
			{
				Name: "validname2",
			},
			{
				Name: "validname3",
			},
		}
		validateSpecValuesHaveErr(vdb, false)
	})

	// validate immutable fields
	It("should succeed without changing immutable fields", func() {
		vdb := createVDBHelper()
		vdbUpdate := createVDBHelper()
		allErrs := vdb.validateImmutableFields(vdbUpdate)
		Expect(allErrs).Should(BeNil())
	})
	It("should not change kSafety after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.KSafety = KSafety0
		validateImmutableFields(vdbUpdate)
	})
	It("should not change initPolicy after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.InitPolicy = CommunalInitPolicyRevive
		validateImmutableFields(vdbUpdate)
	})
	It("should not change dbName after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.DBName = "newdb"
		validateImmutableFields(vdbUpdate)
	})
	It("should not change dataPath after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.DataPath = "/newpath"
		validateImmutableFields(vdbUpdate)
	})
	It("should not change depot path after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.DepotPath = "/newdepot"
		validateImmutableFields(vdbUpdate)
	})
	It("should not change shardCount after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.ShardCount = 10
		validateImmutableFields(vdbUpdate)
	})
	It("should not change isPrimary after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Subclusters[0].IsPrimary = !vdbUpdate.Spec.Subclusters[0].IsPrimary
		validateImmutableFields(vdbUpdate)
	})
	It("should allow image change if autoRestartVertica is disabled", func() {
		vdb := createVDBHelper()
		vdb.Spec.AutoRestartVertica = false
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Image = "vertica-k8s:v10"
		vdbUpdate.Spec.AutoRestartVertica = false
		allErrs := vdb.validateImmutableFields(vdbUpdate)
		Expect(allErrs).Should(BeNil())
	})
	It("should not change communal.path after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Communal.Path = "s3://nimbusdb/spilchen"
		validateImmutableFields(vdbUpdate)
	})
	It("should not change communal.endpoint after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Communal.Endpoint = "https://minio"
		validateImmutableFields(vdbUpdate)
	})
	It("should not change local.requestSize after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.RequestSize = resource.MustParse("600Gi")
		validateImmutableFields(vdbUpdate)
	})
	It("should not change local.storageClass after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.StorageClass = "MyStorageClass"
		validateImmutableFields(vdbUpdate)
	})

	It("should not have zero matched subcluster names to the old subcluster names", func() {
		vdb := createVDBHelper()
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, Subcluster{
			Name: "sc2",
			Size: 3,
		})
		vdbUpdate := createVDBHelper()
		sc := &vdbUpdate.Spec.Subclusters[0]
		sc.Name = "sc1new"
		vdbUpdate.Spec.Subclusters = append(vdbUpdate.Spec.Subclusters, Subcluster{
			Name: "sc2new",
			Size: 3,
		})
		allErrs := vdb.validateImmutableFields(vdbUpdate)
		Expect(allErrs).ShouldNot(BeNil())
	})

	It("should only allow certain values for initPolicy", func() {
		vdb := createVDBHelper()
		vdb.Spec.InitPolicy = "Random"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should only allow nodePort if serviceType allows for it", func() {
		vdb := createVDBHelper()
		vdb.Spec.Subclusters[0].ServiceType = v1.ServiceTypeNodePort
		vdb.Spec.Subclusters[0].NodePort = 30000
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.Subclusters[0].ServiceType = v1.ServiceTypeClusterIP
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should default endpoint for google cloud", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Path = "gs://some-bucket/db"
		vdb.Spec.Communal.Endpoint = ""
		vdb.Default()
		Expect(vdb.Spec.Communal.Endpoint).Should(Equal(DefaultGCloudEndpoint))
	})

	It("should prevent volumeMount paths to use same path as internal mount points", func() {
		vdb := createVDBHelper()
		vdb.Spec.VolumeMounts = []v1.VolumeMount{
			{MountPath: paths.LogPath}}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.VolumeMounts = []v1.VolumeMount{
			{MountPath: vdb.Spec.Local.DataPath}}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.VolumeMounts = []v1.VolumeMount{
			{MountPath: fmt.Sprintf("%s/my.cert", paths.CertsRoot)}}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.VolumeMounts = []v1.VolumeMount{
			{MountPath: "/good/path"}}
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should detect when Kerberos is partially setup", func() {
		kerberosSetup := [][]string{{"", "realm", ""}, {"", "", "principal"},
			{"secret", "realm", ""}, {"secret", "", "principal"}, {"", "realm", "principal"}}

		for _, vals := range kerberosSetup {
			vdb := createVDBHelper()
			vdb.Spec.KerberosSecret = vals[0]
			vdb.Spec.Communal.KerberosRealm = vals[1]
			vdb.Spec.Communal.KerberosServiceName = vals[2]
			validateSpecValuesHaveErr(vdb, true)
		}
	})

	It("should allow upgradePolicy to be changed when upgrade is not in progress", func() {
		vdbUpdate := createVDBHelper()
		vdbOrig := createVDBHelper()
		vdbOrig.Spec.UpgradePolicy = OfflineUpgrade
		vdbUpdate.Spec.UpgradePolicy = OnlineUpgrade
		allErrs := vdbOrig.validateImmutableFields(vdbUpdate)
		Expect(allErrs).Should(BeNil())

		vdbUpdate.Status.Conditions = make([]VerticaDBCondition, ImageChangeInProgressIndex+1)
		vdbUpdate.Status.Conditions[ImageChangeInProgressIndex] = VerticaDBCondition{
			Status: v1.ConditionTrue,
		}
		allErrs = vdbOrig.validateImmutableFields(vdbUpdate)
		Expect(allErrs).ShouldNot(BeNil())
	})

	It("should fail for various issues with temporary subcluster routing template", func() {
		vdb := createVDBHelper()
		vdb.Spec.TemporarySubclusterRouting.Template.Name = vdb.Spec.Subclusters[0].Name
		vdb.Spec.TemporarySubclusterRouting.Template.Size = 1
		vdb.Spec.TemporarySubclusterRouting.Template.IsPrimary = false
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Template.Name = "transient"
		vdb.Spec.TemporarySubclusterRouting.Template.Size = 0
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Template.Size = 1
		vdb.Spec.TemporarySubclusterRouting.Template.IsPrimary = true
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Template.IsPrimary = false
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should fail setting template and names in temporary routing", func() {
		vdb := createVDBHelper()
		vdb.Spec.TemporarySubclusterRouting.Template.Name = "my-transient-sc"
		vdb.Spec.TemporarySubclusterRouting.Template.Size = 1
		vdb.Spec.TemporarySubclusterRouting.Template.IsPrimary = false
		vdb.Spec.TemporarySubclusterRouting.Names = []string{vdb.Spec.Subclusters[0].Name}
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should fail if temporary routing to a subcluster doesn't exist", func() {
		vdb := createVDBHelper()
		const ValidScName = "sc1"
		const InvalidScName = "notexists"
		vdb.Spec.Subclusters[0].Name = ValidScName
		vdb.Spec.TemporarySubclusterRouting.Names = []string{InvalidScName}
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Names = []string{ValidScName, InvalidScName}
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Names = []string{ValidScName}
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should prevent change to temporarySubclusterRouting when upgrade is in progress", func() {
		vdbUpdate := createVDBHelper()
		vdbOrig := createVDBHelper()

		vdbUpdate.Status.Conditions = make([]VerticaDBCondition, ImageChangeInProgressIndex+1)
		vdbUpdate.Status.Conditions[ImageChangeInProgressIndex] = VerticaDBCondition{
			Status: v1.ConditionTrue,
		}

		vdbUpdate.Spec.TemporarySubclusterRouting.Names = []string{"sc1", "sc2"}
		vdbOrig.Spec.TemporarySubclusterRouting.Names = []string{"sc3", "sc4"}
		allErrs := vdbOrig.validateImmutableFields(vdbUpdate)
		Expect(allErrs).ShouldNot(BeNil())

		vdbUpdate.Spec.TemporarySubclusterRouting.Names = vdbOrig.Spec.TemporarySubclusterRouting.Names
		vdbUpdate.Spec.TemporarySubclusterRouting.Template.Name = "transient-sc"
		vdbOrig.Spec.TemporarySubclusterRouting.Template.Name = "another-name-transient-sc"
		allErrs = vdbOrig.validateImmutableFields(vdbUpdate)
		Expect(allErrs).ShouldNot(BeNil())
	})

	It("should error out if service specific fields are different in subclusters with matching serviceNames", func() {
		vdb := createVDBHelper()
		const ServiceName = "main"
		vdb.Spec.Subclusters = []Subcluster{
			{
				Name:               "sc1",
				Size:               2,
				IsPrimary:          true,
				ServiceName:        ServiceName,
				ServiceType:        "NodePort",
				NodePort:           30008,
				ExternalIPs:        []string{"8.1.2.3", "8.2.4.6"},
				LoadBalancerIP:     "9.0.1.2",
				ServiceAnnotations: map[string]string{"foo": "bar", "dib": "dab"},
			},
			{
				Name:               "sc2",
				Size:               1,
				IsPrimary:          false,
				ServiceName:        ServiceName,
				ServiceType:        "ClusterIP",
				NodePort:           30009,
				ExternalIPs:        []string{"8.1.2.3", "7.2.4.6"},
				LoadBalancerIP:     "9.3.4.5",
				ServiceAnnotations: map[string]string{"foo": "bar", "dib": "baz"},
			},
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].ServiceType = vdb.Spec.Subclusters[0].ServiceType
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].NodePort = vdb.Spec.Subclusters[0].NodePort
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].ExternalIPs[1] = vdb.Spec.Subclusters[0].ExternalIPs[1]
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].LoadBalancerIP = vdb.Spec.Subclusters[0].LoadBalancerIP
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].ServiceAnnotations = vdb.Spec.Subclusters[0].ServiceAnnotations
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should allow different serviceTypes if the serviceName isn't filled in", func() {
		vdb := createVDBHelper()
		vdb.Spec.Subclusters = []Subcluster{
			{
				Name:        "sc1",
				Size:        2,
				IsPrimary:   true,
				ServiceType: "NodePort",
				NodePort:    30008,
			},
			{
				Name:        "sc2",
				Size:        1,
				IsPrimary:   false,
				ServiceType: "ClusterIP",
			},
		}
		validateSpecValuesHaveErr(vdb, false)
	})

	It("prevent transient subcluster having a different name then the template", func() {
		vdb := createVDBHelper()
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Size: 1, IsPrimary: true},
			{Name: "sc2", Size: 1, IsPrimary: false, IsTransient: true},
		}
		vdb.Spec.TemporarySubclusterRouting.Template = Subcluster{
			Name:      "transient",
			Size:      1,
			IsPrimary: false,
		}
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should fill in the default serviceName if omitted", func() {
		vdb := MakeVDB()
		Expect(vdb.Spec.Subclusters[0].ServiceName).Should(Equal(""))
		vdb.Default()
		Expect(vdb.Spec.Subclusters[0].ServiceName).Should(Equal(vdb.Spec.Subclusters[0].Name))
	})
})

func createVDBHelper() *VerticaDB {
	vdb := MakeVDB()
	// check other field values in the MakeVDB function
	sc := &vdb.Spec.Subclusters[0]
	sc.IsPrimary = true
	vdb.Spec.KSafety = KSafety1
	requestSize, _ := resource.ParseQuantity("500Gi")
	vdb.Spec.Local.RequestSize = requestSize
	vdb.Status.Subclusters = []SubclusterStatus{}
	vdb.Status.Subclusters = append(vdb.Status.Subclusters, SubclusterStatus{AddedToDBCount: 1})
	return vdb
}

func validateSpecValuesHaveErr(vdb *VerticaDB, hasErr bool) {
	allErrs := vdb.validateVerticaDBSpec()
	if hasErr {
		ExpectWithOffset(1, allErrs).ShouldNot(BeNil())
	} else {
		ExpectWithOffset(1, allErrs).Should(BeNil())
	}
}

func validateImmutableFields(vdbUpdate *VerticaDB) {
	vdb := createVDBHelper()
	allErrs := vdb.validateImmutableFields(vdbUpdate)
	Expect(allErrs).ShouldNot(BeNil())
}
