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

package v1

import (
	"fmt"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

var _ = Describe("verticadb_webhook", func() {
	const (
		oldSecret = "old-secret"
		newSecret = "new-secret"
		oldMode   = "verify_ca"
		newMode   = "verify_full"
	)

	var (
		oldVdb1 *VerticaDB
		newVdb1 *VerticaDB
	)

	BeforeEach(func() {
		oldVdb1 = MakeVDB()
		newVdb1 = oldVdb1.DeepCopy()
		// Enable TLS
		oldVdb1.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		newVdb1.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		// Set initial TLS secrets and modes
		oldVdb1.Spec.HTTPSNMATLS = &TLSConfigSpec{Secret: oldSecret, Mode: oldMode}
		oldVdb1.Spec.ClientServerTLS = &TLSConfigSpec{Secret: oldSecret, Mode: oldMode}
		newVdb1.Spec.HTTPSNMATLS = &TLSConfigSpec{Secret: oldSecret, Mode: oldMode}
		newVdb1.Spec.ClientServerTLS = &TLSConfigSpec{Secret: oldSecret, Mode: oldMode}
		// Set status fields to match spec
		oldVdb1.Status.TLSConfigs = []TLSConfigStatus{
			{Name: HTTPSNMATLSConfigName, Secret: oldSecret, Mode: oldMode},
			{Name: ClientServerTLSConfigName, Secret: oldSecret, Mode: oldMode},
		}
		newVdb1.Status.TLSConfigs = []TLSConfigStatus{
			{Name: HTTPSNMATLSConfigName, Secret: oldSecret, Mode: oldMode},
			{Name: ClientServerTLSConfigName, Secret: oldSecret, Mode: oldMode},
		}
	})

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

	It("should not have invalid vdb name", func() {
		vdb := createVDBHelper()
		// service object names cannot start with a numeric character
		vdb.ObjectMeta.Name = "1" + vdb.ObjectMeta.Name
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have invalid subcluster service name", func() {
		vdb := createVDBHelper()
		// service object names cannot include '_' character
		vdb.Spec.Subclusters[0].ServiceName = "sc_svc"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have invalid external service name (concatenated by a valid vdb name"+
		" and valid subcluster service name if used alone as a service name)", func() {
		vdb := createVDBHelper()
		// this serviceName alone is valid when used as service object name
		// because it consists of lower case alphanumeric characters or '-',
		// starts with an alphabetic character, ends with an alphanumeric character,
		// and is not longer than 63 characters (see DNS-1035 label requirement)
		vdb.Spec.Subclusters[0].ServiceName = "a012345678901234567890123456789" +
			"012345678901234567890123456789"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should allow auto-generated service name from subcluster name", func() {
		vdb := createVDBHelper()
		// all '_' in subcluster names are replaced by '-'
		// thus the auto-generated service name should be valid
		vdb.Spec.Subclusters[0].Name = "default_subcluster"
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should have at least one primary subcluster", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.Type = SecondarySubcluster
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should have valid subcluster type", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.Type = "invalid"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have 0 pod when kSafety is 0", func() {
		vdb := createVDBHelper()
		vdb.Annotations[vmeta.KSafetyAnnotation] = "0"
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 0
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have more than 3 pods when kSafety is 0", func() {
		vdb := createVDBHelper()
		vdb.Annotations[vmeta.KSafetyAnnotation] = "0"
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
		vdb.Spec.Communal.Path = "http://nimbusdb/cchen"
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.Communal.Path = ""
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have invalid communal endpoint", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Endpoint = "s3://minio"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should allow an empty communal endpoint", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Endpoint = ""
		vdb.Spec.Communal.Path = "s3://my-bucket"
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should not have invalid server-side encryption type", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.S3ServerSideEncryption = "fakessetype"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should have s3SseKmsKeyId set when server-side encryption type is SSE-KMS", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.S3ServerSideEncryption = SseKMS
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Communal.AdditionalConfig = map[string]string{
			S3SseKmsKeyID: "",
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Communal.AdditionalConfig[S3SseKmsKeyID] = "randomid"
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should have s3SseCustomerKeySecret set when server-side encryption type is SSE-C", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.S3ServerSideEncryption = SseC
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Communal.S3SseCustomerKeySecret = "ssecustomersecret"
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should succeed when server-side encryption type is SSE-S3", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.S3ServerSideEncryption = SseS3
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should skip sse validation if communal storage is not s3 or sse type is not specified", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.S3ServerSideEncryption = ""
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.Communal.S3ServerSideEncryption = "faketype"
		vdb.Spec.Communal.Path = GCloudPrefix + "randompath"
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should allow valid additionalBuckets", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Path = AzurePrefix + "mainbucket"
		vdb.Spec.AdditionalBuckets = []CommunalStorage{
			{
				Path:             S3Prefix + "extrabucket",
				Endpoint:         "https://s3.example.com",
				Region:           "us-east-1",
				CredentialSecret: "extrasecret",
			},
		}
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should require additionalBuckets to use a different protocol than communal for gs and azb", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Path = GCloudPrefix + "mainbucket"
		vdb.Spec.Communal.Endpoint = "https://gs.example.com"
		vdb.Spec.Communal.CredentialSecret = "mainsecret"

		// Valid: additional bucket uses s3, communal uses gs
		vdb.Spec.AdditionalBuckets = []CommunalStorage{
			{
				Path:             S3Prefix + "extrabucket",
				Endpoint:         "https://s3.example.com",
				Region:           "us-east-1",
				CredentialSecret: "extrasecret",
			},
		}
		validateSpecValuesHaveErr(vdb, false)

		// Invalid: additional bucket uses same protocol as communal
		vdb.Spec.AdditionalBuckets[0].Path = GCloudPrefix + "extrabucket"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should require all additionalBuckets fields and a valid protocol", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Path = GCloudPrefix + "mainbucket"
		vdb.Spec.Communal.Endpoint = "https://gs.example.com"
		vdb.Spec.Communal.CredentialSecret = "mainsecret"

		// Invalid: missing required fields
		vdb.Spec.AdditionalBuckets = []CommunalStorage{{}}
		validateSpecValuesHaveErr(vdb, true)

		// Invalid: invalid protocol
		vdb.Spec.AdditionalBuckets[0] = CommunalStorage{
			Path:             "ftp://bucket",
			Endpoint:         "https://ftp.example.com",
			Region:           "us-east-1",
			CredentialSecret: "secret",
		}
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have proxy replicas <= 0 if proxy is enabled", func() {
		vdb := createVDBHelper()
		vdb.Annotations[vmeta.UseVProxyAnnotation] = trueString
		sc1 := &vdb.Spec.Subclusters[0]
		*sc1.Proxy.Replicas = -1
		validateSpecValuesHaveErr(vdb, true)
		*sc1.Proxy.Replicas = 0
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should set proxy spec if proxy is enabled", func() {
		vdb := createVDBHelper()
		vdb.Annotations[vmeta.UseVProxyAnnotation] = trueString
		vdb.Spec.Proxy.Image = ""
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Proxy = nil
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have invalid value for proxy log level", func() {
		vdb := createVDBHelper()
		vdb.Annotations[vmeta.UseVProxyAnnotation] = trueString
		vdb.Annotations[vmeta.VProxyLogLevelAnnotation] = "INVALID_VALUE"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have duplicate parms in communal.AdditionalConfig", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.AdditionalConfig = map[string]string{
			"awsauth":     "xxxx:xxxx",
			"awsendpoint": "s3.amazonaws.com",
			"AWSauth":     "xxxx:xxxx",
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Communal.AdditionalConfig = map[string]string{
			"awsauth":     "xxxx:xxxx",
			"awsendpoint": "s3.amazonaws.com",
		}
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should have valid subcluster name", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.Name = "default-subcluster"
		validateSpecValuesHaveErr(vdb, false)
	})
	It("should not have invalid subcluster name", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.Name = "defaultsubcluster_"
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should be allowed to have empty credentialsecret", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.CredentialSecret = ""
		validateSpecValuesHaveErr(vdb, false)
	})
	It("should not have nodePort smaller than 30000", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.ServiceType = v1.ServiceTypeNodePort
		sc.ClientNodePort = 5555
		validateSpecValuesHaveErr(vdb, true)
	})
	It("should not have nodePort bigger than 32767", func() {
		vdb := createVDBHelper()
		sc := &vdb.Spec.Subclusters[0]
		sc.ServiceType = v1.ServiceTypeNodePort
		sc.ClientNodePort = 55555
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

	It("should not have negative createdb timeout", func() {
		vdb := MakeVDB()
		annotationName := vmeta.CreateDBTimeoutAnnotation
		vdb.Annotations[annotationName] = "-1"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have negative drain timeout", func() {
		vdb := MakeVDB()
		annotationName := vmeta.ActiveConnectionsDrainSecondsAnnotation
		vdb.Annotations[annotationName] = "-1"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not include UID in path if revive_db", func() {
		vdb := MakeVDB()
		annotationName := vmeta.IncludeUIDInPathAnnotation
		vdb.Annotations[annotationName] = trueString
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.InitPolicy = CommunalInitPolicyRevive
		validateSpecValuesHaveErr(vdb, true)
	})

	// validate immutable fields
	It("should succeed without changing immutable fields", func() {
		vdb := createVDBHelper()
		vdbUpdate := createVDBHelper()
		allErrs := vdb.validateImmutableFields(vdbUpdate)
		Expect(allErrs).Should(BeNil())
	})
	It("should not change initPolicy after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.InitPolicy = CommunalInitPolicyRevive
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change dbName after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.DBName = "newdb"
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change dataPath after DB init", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.DataPath = "/newpath"
		validateImmutableFields(vdbUpdate, false)
		resetStatusConditionsForDBInitialized(vdbUpdate)
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change depot path after DB init", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.DepotPath = "/newdepot"
		validateImmutableFields(vdbUpdate, false)
		resetStatusConditionsForDBInitialized(vdbUpdate)
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change catalog path after DB init", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.CatalogPath = "/newcatalog"
		validateImmutableFields(vdbUpdate, false)
		resetStatusConditionsForDBInitialized(vdbUpdate)
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change shardCount after DB init", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.ShardCount = 10
		validateImmutableFields(vdbUpdate, false)
		resetStatusConditionsForDBInitialized(vdbUpdate)
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change isPrimary after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Subclusters[0].Type = SecondarySubcluster
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change deployment type from vclusterops to admintools after creation", func() {
		vdbOrig := createVDBHelper()
		vdbOrig.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdbUpdate := createVDBHelper()
		// cannot change from vclusterops to admintools
		checkErrorsForImmutableFields(vdbOrig, vdbUpdate, true)
		// can change from admintools to vclusterops
		checkErrorsForImmutableFields(vdbUpdate, vdbOrig, false)
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
		vdbUpdate.Spec.Communal.Path = "s3://nimbusdb/chen"
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change communal.endpoint after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Communal.Endpoint = "https://minio"
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change communal.s3ServerSideEncryption after creation", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.S3ServerSideEncryption = SseS3
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Communal.S3ServerSideEncryption = SseKMS
		allErrs := vdb.validateImmutableFields(vdbUpdate)
		Expect(allErrs).ShouldNot(BeNil())
	})
	It("should not change local.storageClass after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.StorageClass = "MyStorageClass"
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change proxy.image after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Proxy.Image = "NewProxyImage"
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change annotation vertica.com/use-client-proxy after creation", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Annotations[vmeta.UseVProxyAnnotation] = "NewValue"
		validateImmutableFields(vdbUpdate, true)
	})
	It("should not change local.depotVolume after DB init", func() {
		vdbUpdate := createVDBHelper()
		vdbUpdate.Spec.Local.DepotVolume = EmptyDir
		validateImmutableFields(vdbUpdate, false)
		resetStatusConditionsForDBInitialized(vdbUpdate)
		validateImmutableFields(vdbUpdate, true)
	})
	It("httpsNMATLS.secret cannot be changed when cert rotation is in progress", func() {
		vdb := MakeVDBForCertRotationEnabled()
		oldVdb := vdb.DeepCopy()
		oldVdb.Spec.HTTPSNMATLS.Secret = oldSecret
		vdb.Spec.HTTPSNMATLS.Secret = "newSecretValue"
		resetStatusConditionsForCertRotationInProgress(vdb)
		allErrs := vdb.validateImmutableFields(oldVdb)
		Expect(allErrs).ShouldNot(BeNil())
	})

	It("should only allow tls config related changes when tls config update is in progress", func() {
		oldVdb := MakeVDBForCertRotationEnabled()
		oldVdb.Status.Conditions = append(oldVdb.Status.Conditions, metav1.Condition{
			Type:   TLSConfigUpdateInProgress,
			Status: metav1.ConditionTrue,
		})
		oldVdb.Spec.HTTPSNMATLS.Secret = "secret1"
		oldVdb.Spec.ClientServerTLS.Secret = "secret1"
		oldVdb.Spec.ClientServerTLS.Mode = tlsModeVerifyCA

		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "default", Size: 3, Type: PrimarySubcluster},
			{Name: "sc1", Size: 1, Type: SecondarySubcluster},
		}
		// Only cert-rotation-related changes: allowed
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.HTTPSNMATLS.Secret = "secret2"
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())

		newVdb.Spec.ClientServerTLS.Secret = "secret2"
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())

		newVdb.Spec.ClientServerTLS.Mode = tlsModeTryVerify
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())

		// SomeOtherField changes: forbidden
		newVdb = oldVdb.DeepCopy()
		newVdb.Spec.Subclusters[1].Size = 3
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
	})

	It("should not allow disabling mutual TLS after it's enabled", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.HTTPSNMATLS.Secret = "enabled"
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.HTTPSNMATLS.Secret = ""
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
	})

	It("should not allow cert-rotation-related changes when cert rotation is disabled", func() {
		oldVdb := MakeVDB()
		Expect(vmeta.UseTLSAuth(oldVdb.Annotations)).Should(BeFalse())

		oldVdb.Spec.HTTPSNMATLS.Secret = "old-secret"
		oldVdb.Spec.ClientServerTLS.Secret = "old-secret"
		oldVdb.Spec.ClientServerTLS.Mode = tlsModeVerifyCA
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "default", Size: 3, Type: PrimarySubcluster},
		}
		// No cert-rotation-related changes is allowed
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.HTTPSNMATLS.Secret = newSecret
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())

		newVdb.Spec.ClientServerTLS.Secret = newSecret
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())

		newVdb.Spec.ClientServerTLS.Mode = tlsModeTryVerify
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
	})

	It("should return error if both httpsNMATLS and clientServerTLS are changed at the same time", func() {
		oldVdb := MakeVDBForCertRotationEnabled()
		oldVdb.Spec.HTTPSNMATLS.Secret = oldSecret
		oldVdb.Spec.ClientServerTLS.Secret = oldSecret
		newVdb := oldVdb.DeepCopy()
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())
		newVdb.Spec.HTTPSNMATLS.Secret = newSecret
		newVdb.Spec.ClientServerTLS.Secret = newSecret
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())
		newVdb.Status.TLSConfigs = []TLSConfigStatus{
			{Name: HTTPSNMATLSConfigName, Secret: oldSecret, Mode: oldMode},
			{Name: ClientServerTLSConfigName, Secret: oldSecret, Mode: oldMode},
		}
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Expect(allErrs).Should(HaveLen(1))
		Expect(allErrs[0].Error()).To(ContainSubstring("cannot change both httpsNMATLS and clientServerTLS at the same time"))
	})

	It("should not change a tls secret to empty string", func() {
		oldVdb := MakeVDBForCertRotationEnabled()
		oldVdb.Spec.HTTPSNMATLS.Secret = oldSecret
		oldVdb.Spec.ClientServerTLS.Secret = oldSecret
		newVdb := oldVdb.DeepCopy()
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())
		newVdb.Spec.HTTPSNMATLS.Secret = ""
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Expect(allErrs).Should(HaveLen(1))
		newVdb.Spec.HTTPSNMATLS.Secret = oldSecret
		newVdb.Spec.ClientServerTLS.Secret = ""
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Expect(allErrs).Should(HaveLen(1))
		newVdb.Spec.HTTPSNMATLS.Secret = ""
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Expect(allErrs).Should(HaveLen(2))
		Expect(allErrs[0].Error()).To(ContainSubstring("cannot change httpsNMATLS.secret to empty value"))
		Expect(allErrs[1].Error()).To(ContainSubstring("cannot change clientServerTLS.secret to empty value"))
	})

	It("should not allow changing nmaTLSSecret", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.NMATLSSecret = "old-nma"
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.NMATLSSecret = "new-nma"
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
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

	It("should not have two or more subclusters whose names only differ by `-` and `_`", func() {
		vdb := createVDBHelper()
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, Subcluster{
			Name: "default_subcluster",
			Size: 3,
		})
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, Subcluster{
			Name: "default-subcluster",
			Size: 3,
		})
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should only allow certain values for initPolicy", func() {
		vdb := createVDBHelper()
		vdb.Spec.InitPolicy = "Random"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should validate restorePoint when initPolicy is \"Revive\" and a restore is intended", func() {
		vdb := createVDBHelper()
		vdb.Spec.InitPolicy = "Revive"
		vdb.Spec.RestorePoint = &RestorePointPolicy{}
		// archive is not provided
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.RestorePoint.Archive = "archive"
		// neither id nor index is provided
		validateSpecValuesHaveErr(vdb, true)
		// only invalid index is provided
		vdb.Spec.RestorePoint.Index = -1
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.RestorePoint.Index = 0
		validateSpecValuesHaveErr(vdb, true)
		// both id and index are provided
		vdb.Spec.RestorePoint.ID = "id"
		vdb.Spec.RestorePoint.Index = 1
		validateSpecValuesHaveErr(vdb, true)
		// only id is provided
		vdb.Spec.RestorePoint.Index = 0
		validateSpecValuesHaveErr(vdb, false)
		// only index is provided
		vdb.Spec.RestorePoint.ID = ""
		vdb.Spec.RestorePoint.Index = 1
		validateSpecValuesHaveErr(vdb, false)
		// archive name cannot have invalid chars
		vdb.Spec.RestorePoint.Archive = "bad@archive"
		validateSpecValuesHaveErr(vdb, true)
		// dash character is valid in archive name
		vdb.Spec.RestorePoint.Archive = "good-archive"
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.RestorePoint.Archive = "archive"
		validateSpecValuesHaveErr(vdb, false)
		// numRestorePoints 0 or greater
		vdb.Spec.RestorePoint.NumRestorePoints = -1
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.RestorePoint.NumRestorePoints = 0
		validateSpecValuesHaveErr(vdb, false)
		// when db is already initialized, we shouldn't report an error about missing archive or restore point
		vdb2 := createVDBHelper()
		vdb2.Spec.InitPolicy = "Revive"
		vdb2.Spec.RestorePoint = &RestorePointPolicy{}
		resetStatusConditionsForDBInitialized(vdb2)
		// archive is not provided
		validateSpecValuesHaveErr(vdb2, false)
		vdb2.Spec.RestorePoint.Archive = "archive2"
		// neither id nor index is provided
		validateSpecValuesHaveErr(vdb2, false)
	})

	It("should only allow nodePort if serviceType allows for it", func() {
		vdb := createVDBHelper()
		vdb.Spec.Subclusters[0].ServiceType = v1.ServiceTypeNodePort
		vdb.Spec.Subclusters[0].ClientNodePort = 30000
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.Subclusters[0].ServiceType = v1.ServiceTypeClusterIP
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should only allow valid ServiceHTTPSPort and ServiceClientPort", func() {
		vdb := createVDBHelper()
		vdb.Spec.ServiceHTTPSPort = -1
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.ServiceHTTPSPort = 8443
		vdb.Spec.ServiceClientPort = -1
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.ServiceClientPort = 5433
		vdb.Spec.Subclusters[0].ServiceHTTPSPort = -1
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[0].ServiceHTTPSPort = 8443
		vdb.Spec.Subclusters[0].ServiceClientPort = -1
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[0].ServiceClientPort = 5433
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should default endpoint for google cloud", func() {
		vdb := createVDBHelper()
		vdb.Spec.Communal.Path = "gs://some-bucket/db"
		vdb.Spec.Communal.Endpoint = ""
		vdb.Default()
		Expect(vdb.Spec.Communal.Endpoint).Should(Equal(DefaultGCloudEndpoint))
	})

	It("should fill in the default sandbox image if omitted", func() {
		vdb := MakeVDB()
		const (
			sb1 = "sb1"
			sb2 = "sb2"
			sb3 = "sb3"
			img = "vertica:test"
		)
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "default", Size: 1, Type: PrimarySubcluster},
			{Name: "sc1", Size: 1, Type: SecondarySubcluster},
			{Name: "sc2", Size: 1, Type: SecondarySubcluster},
			{Name: "sc3", Size: 1, Type: SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: sb1, Image: img, Subclusters: []SandboxSubcluster{{Name: vdb.Spec.Subclusters[1].Name}}},
			{Name: sb2, Subclusters: []SandboxSubcluster{{Name: vdb.Spec.Subclusters[2].Name}}},
			{Name: sb3, Subclusters: []SandboxSubcluster{{Name: vdb.Spec.Subclusters[3].Name}}},
		}
		vdb.Default()
		Expect(vdb.Spec.Sandboxes[0].Image).Should(Equal(img))
		Expect(vdb.Spec.Sandboxes[1].Image).Should(Equal(vdb.Spec.Image))
		Expect(vdb.Spec.Sandboxes[2].Image).Should(Equal(vdb.Spec.Image))
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
			vdb.Spec.Communal.AdditionalConfig[vmeta.KerberosRealmConfig] = vals[1]
			vdb.Spec.Communal.AdditionalConfig[vmeta.KerberosServiceNameConfig] = vals[2]
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

		resetStatusConditionsForUpgradeInProgress(vdbUpdate)
		allErrs = vdbOrig.validateImmutableFields(vdbUpdate)
		Expect(allErrs).ShouldNot(BeNil())
	})

	It("should fail for various issues with temporary subcluster routing template", func() {
		vdb := createVDBHelper()
		vdb.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Template: Subcluster{
				Name: vdb.Spec.Subclusters[0].Name,
				Size: 1,
				Type: SecondarySubcluster,
			},
		}
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Template.Name = "transient"
		vdb.Spec.TemporarySubclusterRouting.Template.Size = 0
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Template.Size = 1
		vdb.Spec.TemporarySubclusterRouting.Template.Type = PrimarySubcluster
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Template.Type = SecondarySubcluster
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should fail setting template and names in temporary routing", func() {
		vdb := createVDBHelper()
		vdb.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Template: Subcluster{
				Name: "my-transient-sc",
				Size: 1,
				Type: SecondarySubcluster,
			},
			Names: []string{vdb.Spec.Subclusters[0].Name},
		}
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should fail if you set or clear the temporarySubclusterRouting field", func() {
		vdbOrig := MakeVDB()
		vdbOrig.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Names: []string{"sc1"},
		}
		vdbUpdate := MakeVDB()
		vdbUpdate.Spec.TemporarySubclusterRouting = nil
		resetStatusConditionsForUpgradeInProgress(vdbUpdate)
		resetStatusConditionsForUpgradeInProgress(vdbOrig)
		allErrs := vdbOrig.validateImmutableFields(vdbUpdate)
		Ω(allErrs).ShouldNot(BeNil())

		// Swap the case
		vdbUpdate.Spec.TemporarySubclusterRouting = vdbOrig.Spec.TemporarySubclusterRouting
		vdbOrig.Spec.TemporarySubclusterRouting = nil
		allErrs = vdbOrig.validateImmutableFields(vdbUpdate)
		Ω(allErrs).ShouldNot(BeNil())
	})

	It("should fail if temporary routing to a subcluster doesn't exist", func() {
		vdb := createVDBHelper()
		const ValidScName = "sc1"
		const InvalidScName = "notexists"
		vdb.Spec.Subclusters[0].Name = ValidScName
		vdb.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Names: []string{InvalidScName},
		}
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Names = []string{ValidScName, InvalidScName}
		validateSpecValuesHaveErr(vdb, true)

		vdb.Spec.TemporarySubclusterRouting.Names = []string{ValidScName}
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should prevent change to temporarySubclusterRouting when upgrade is in progress", func() {
		vdbUpdate := createVDBHelper()
		vdbOrig := createVDBHelper()

		resetStatusConditionsForUpgradeInProgress(vdbUpdate)

		vdbUpdate.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Names: []string{"sc1", "sc2"},
		}
		vdbOrig.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Names: []string{"sc3", "sc4"},
		}
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
		vdb.Annotations[vmeta.StrictKSafetyCheckAnnotation] = strconv.FormatBool(false)
		const ServiceName = "main"
		vdb.Spec.Subclusters = []Subcluster{
			{
				Name:               "sc1",
				Size:               2,
				Type:               PrimarySubcluster,
				ServiceName:        ServiceName,
				ServiceType:        "NodePort",
				ClientNodePort:     30008,
				ExternalIPs:        []string{"8.1.2.3", "8.2.4.6"},
				LoadBalancerIP:     "9.0.1.2",
				ServiceAnnotations: map[string]string{"foo": "bar", "dib": "dab"},
			},
			{
				Name:               "sc2",
				Size:               1,
				Type:               SecondarySubcluster,
				ServiceName:        ServiceName,
				ServiceType:        "ClusterIP",
				ClientNodePort:     30009,
				ExternalIPs:        []string{"8.1.2.3", "7.2.4.6"},
				LoadBalancerIP:     "9.3.4.5",
				ServiceAnnotations: map[string]string{"foo": "bar", "dib": "baz"},
			},
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].ServiceType = vdb.Spec.Subclusters[0].ServiceType
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].ClientNodePort = vdb.Spec.Subclusters[0].ClientNodePort
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].ExternalIPs[1] = vdb.Spec.Subclusters[0].ExternalIPs[1]
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].LoadBalancerIP = vdb.Spec.Subclusters[0].LoadBalancerIP
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[1].ServiceAnnotations = vdb.Spec.Subclusters[0].ServiceAnnotations
		validateSpecValuesHaveErr(vdb, false)
		// make the k-safety check strict
		vdb.Annotations[vmeta.StrictKSafetyCheckAnnotation] = strconv.FormatBool(true)
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[0].Size = 5
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should allow different serviceTypes if the serviceName isn't filled in", func() {
		vdb := createVDBHelper()
		vdb.Spec.Subclusters = []Subcluster{
			{
				Name:           "sc1",
				Size:           2,
				Type:           PrimarySubcluster,
				ServiceType:    "NodePort",
				ClientNodePort: 30008,
			},
			{
				Name:        "sc2",
				Size:        1,
				Type:        PrimarySubcluster,
				ServiceType: "ClusterIP",
			},
		}
		validateSpecValuesHaveErr(vdb, false)
	})

	It("prevent transient subcluster having a different name then the template", func() {
		vdb := createVDBHelper()
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Size: 1, Type: PrimarySubcluster},
			{Name: "sc2", Size: 1, Type: TransientSubcluster},
		}
		vdb.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Template: Subcluster{
				Name: "transient",
				Size: 1,
				Type: SecondarySubcluster,
			},
		}
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should fill in the default serviceName if omitted", func() {
		vdb := MakeVDB()
		Expect(vdb.Spec.Subclusters[0].ServiceName).Should(Equal(""))
		vdb.Default()
		Expect(vdb.Spec.Subclusters[0].ServiceName).Should(Equal(vdb.Spec.Subclusters[0].Name))
	})

	It("should fill in the default proxy if omitted", func() {
		vdb := MakeVDB()
		vdb.Spec.Proxy = &Proxy{}
		vdb.Default()
		Expect(vdb.Spec.Proxy).Should(BeNil())
		vdb.Annotations[vmeta.UseVProxyAnnotation] = vmeta.UseVProxyAnnotationTrue
		vdb.Spec.Proxy = nil
		vdb.Spec.Subclusters[0].Proxy = nil
		vdb.Default()
		Expect(vdb.Spec.Proxy).ShouldNot(BeNil())
		Expect(vdb.Spec.Proxy.Image).Should(Equal(VProxyDefaultImage))
		Expect(vdb.Spec.Subclusters[0].Proxy).ShouldNot(BeNil())
		Expect(*vdb.Spec.Subclusters[0].Proxy.Replicas).Should(Equal(int32(VProxyDefaultReplicas)))
		Expect(*vdb.Spec.Subclusters[0].Proxy.Resources).Should(Equal(v1.ResourceRequirements{}))
	})

	It("should prevent negative values for requeueTime", func() {
		vdb := MakeVDB()
		vdb.Annotations[vmeta.RequeueTimeAnnotation] = "-30"
		validateSpecValuesHaveErr(vdb, true)
		vdb.Annotations[vmeta.RequeueTimeAnnotation] = "0"
		vdb.Annotations[vmeta.UpgradeRequeueTimeAnnotation] = "-1"
		validateSpecValuesHaveErr(vdb, true)
		vdb.Annotations[vmeta.UpgradeRequeueTimeAnnotation] = "0"
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should prevent encryptSpreadComm from changing", func() {
		vdbOrig := MakeVDB()
		vdbOrig.Spec.EncryptSpreadComm = EncryptSpreadCommWithVertica
		vdbUpdate := MakeVDB()

		allErrs := vdbOrig.validateImmutableFields(vdbUpdate)
		Expect(allErrs).ShouldNot(BeNil())

		vdbOrig.Spec.EncryptSpreadComm = ""
		allErrs = vdbOrig.validateImmutableFields(vdbUpdate)
		Expect(allErrs).Should(BeNil())
	})

	It("should validate the value of encryptSpreadComm", func() {
		vdb := MakeVDB()
		vdb.Spec.EncryptSpreadComm = "blah"
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.EncryptSpreadComm = ""
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.EncryptSpreadComm = EncryptSpreadCommWithVertica
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.EncryptSpreadComm = EncryptSpreadCommDisabled
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should validate we cannot have invalid paths for depot, data and catalog", func() {
		vdb := MakeVDB()
		vdb.Spec.Local.DataPath = "/home/dbadmin"
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Local.DataPath = "/data"
		vdb.Spec.Local.DepotPath = "/opt/vertica/bin"
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Local.DepotPath = "/depot"
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.Local.CatalogPath = "/opt/vertica/sbin"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have invalid depotVolume type", func() {
		vdb := MakeVDB()
		vdb.Spec.Local.DepotVolume = ""
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.Local.DepotVolume = EmptyDir
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.Local.DepotVolume = PersistentVolume
		validateSpecValuesHaveErr(vdb, false)
		vdb.Spec.Local.DepotVolume = "wrong"
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should not have depotPath equal to dataPath or catalogPath when depot volume is emptyDir", func() {
		vdb := MakeVDB()
		vdb.Spec.Local.DepotVolume = EmptyDir
		vdb.Spec.Local.DataPath = "/vertica"
		vdb.Spec.Local.CatalogPath = "/catalog"
		vdb.Spec.Local.DepotPath = vdb.Spec.Local.DataPath
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should prevent internally generated labels to be overridden", func() {
		vdb := MakeVDB()
		vdb.Spec.Labels = map[string]string{
			vmeta.SubclusterNameLabel: "sc-name",
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Labels = map[string]string{
			vmeta.VDBInstanceLabel: "v",
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Labels = map[string]string{
			vmeta.ClientRoutingLabel: vmeta.ClientRoutingVal,
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Labels = map[string]string{
			"vertica.com/good-label": "val",
		}
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should verify range for verticaHTTPNodePort", func() {
		vdb := MakeVDB()
		vdb.Spec.Subclusters[0].ServiceType = v1.ServiceTypeNodePort
		vdb.Spec.Subclusters[0].VerticaHTTPNodePort = 8443 // Too low
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.Subclusters[0].VerticaHTTPNodePort = 30000 // Okay
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should only allow a single handler to be overidden", func() {
		vdb := MakeVDB()
		vdb.Spec.ReadinessProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				Exec: &v1.ExecAction{
					Command: []string{"vsql", "-c", "select 1"},
				},
				TCPSocket: &v1.TCPSocketAction{
					Port: intstr.FromInt(5433),
				},
			},
		}
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.ReadinessProbeOverride = nil
		vdb.Spec.LivenessProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				GRPC: &v1.GRPCAction{
					Port: 5433,
				},
				HTTPGet: &v1.HTTPGetAction{
					Path: "/health",
				},
			},
		}
		validateSpecValuesHaveErr(vdb, true)
	})

	It("should verify the shard count", func() {
		vdb := MakeVDB()
		vdb.Spec.ShardCount = 0
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.ShardCount = -1
		validateSpecValuesHaveErr(vdb, true)
		vdb.Spec.ShardCount = 1
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should not tolerate case sensitivity for subcluster type", func() {
		vdb := MakeVDB()
		ucPrimary := strings.ToUpper(PrimarySubcluster)
		ucSecondary := strings.ToUpper(SecondarySubcluster)
		Ω(ucPrimary).ShouldNot(Equal(PrimarySubcluster))
		Ω(ucSecondary).ShouldNot(Equal(SecondarySubcluster))
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "pri", Type: ucPrimary},
			{Name: "sec", Type: ucSecondary},
		}
		vdb.Default()
		Ω(vdb.Spec.Subclusters[0].Type).ShouldNot(Equal(PrimarySubcluster))
		Ω(vdb.Spec.Subclusters[1].Type).ShouldNot(Equal(SecondarySubcluster))
	})

	It("should not allow changing of fsGroup/runAsUser after DB init", func() {
		oldVdb := MakeVDB()
		oldFSGroup := int64(1000)
		newFSGroup := int64(1001)
		oldRunAsUser := int64(1002)
		newRunAsUser := int64(1003)
		oldVdb.Spec.PodSecurityContext = &v1.PodSecurityContext{
			FSGroup:   &oldFSGroup,
			RunAsUser: &oldRunAsUser,
		}
		newVdb := MakeVDB()
		newVdb.Spec.PodSecurityContext = &v1.PodSecurityContext{
			FSGroup:   &oldFSGroup,
			RunAsUser: &oldRunAsUser,
		}
		resetStatusConditionsForDBInitialized(oldVdb)
		resetStatusConditionsForDBInitialized(newVdb)
		allErrs := newVdb.validateImmutableFields(oldVdb)
		Ω(allErrs).Should(HaveLen(0))

		newVdb.Spec.PodSecurityContext.FSGroup = &newFSGroup
		allErrs = newVdb.validateImmutableFields(oldVdb)
		Ω(allErrs).ShouldNot(HaveLen(0))
		newVdb.Spec.PodSecurityContext.FSGroup = &oldFSGroup

		newVdb.Spec.PodSecurityContext.FSGroup = nil
		allErrs = newVdb.validateImmutableFields(oldVdb)
		Ω(allErrs).ShouldNot(HaveLen(0))
		newVdb.Spec.PodSecurityContext.FSGroup = &oldFSGroup

		newVdb.Spec.PodSecurityContext.RunAsUser = &newRunAsUser
		allErrs = newVdb.validateImmutableFields(oldVdb)
		Ω(allErrs).ShouldNot(HaveLen(0))

		newVdb.Spec.PodSecurityContext.RunAsUser = nil
		allErrs = newVdb.validateImmutableFields(oldVdb)
		Ω(allErrs).ShouldNot(HaveLen(0))
		newVdb.Spec.PodSecurityContext.RunAsUser = &oldRunAsUser

		newVdb.Spec.PodSecurityContext = nil
		allErrs = newVdb.validateImmutableFields(oldVdb)
		Ω(allErrs).ShouldNot(HaveLen(0))
	})

	It("should not allow setting of runAsUser as root", func() {
		oldVdb := MakeVDB()
		runAsUser := int64(0)
		oldVdb.Spec.PodSecurityContext = &v1.PodSecurityContext{
			RunAsUser: &runAsUser,
		}
		allErrs := oldVdb.validateVerticaDBSpec()
		Ω(allErrs).ShouldNot(HaveLen(0))

		runAsUser++ // Make it non-root
		allErrs = oldVdb.validateVerticaDBSpec()
		Ω(allErrs).Should(HaveLen(0))
	})

	It("should prevent setting the memory limit for the NMA to be less than 1Gi", func() {
		vdb := MakeVDB()
		annotationName := vmeta.GenNMAResourcesAnnotationName(v1.ResourceLimitsMemory)
		vdb.Annotations[annotationName] = "500Mi"
		allErrs := vdb.validateVerticaDBSpec()
		Ω(allErrs).ShouldNot(HaveLen(0))

		vdb.Annotations[annotationName] = "1Gi"
		allErrs = vdb.validateVerticaDBSpec()
		Ω(allErrs).Should(HaveLen(0))
	})

	It("should check for upgradePolicy", func() {
		vdb := MakeVDB()
		vdb.Spec.UpgradePolicy = "NotValid"
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))
		vdb.Spec.UpgradePolicy = OnlineUpgrade
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(0))
	})

	It("should check the validity of the replicaGroups", func() {
		vdb := MakeVDB()
		vdb.Spec.Subclusters[0].Annotations = map[string]string{
			vmeta.ReplicaGroupAnnotation: "invalid-value",
		}
		setOnlineUpgradeInProgress(vdb)
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))
		vdb.Spec.Subclusters[0].Annotations = map[string]string{
			vmeta.ReplicaGroupAnnotation: vmeta.ReplicaGroupAValue,
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(0))
	})

	It("should check subcluster immutability during upgrade", func() {
		newVdb := MakeVDB()
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "a", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "b", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
		}
		setOnlineUpgradeInProgress(newVdb)
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))

		oldVdb := newVdb.DeepCopy()

		// Try to change the size
		newVdb.Spec.Subclusters[0].Size = 33
		newVdb.Spec.Subclusters[1].Size = 1
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(2))

		// Add a new primary subcluster.
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "a", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "b", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "c", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))

		// Add a new secondary subcluster. This should be allowed.
		newVdb.Spec.Subclusters[2].Type = SecondarySubcluster
		newVdb.Spec.Subclusters[2].Annotations = map[string]string{
			vmeta.ReplicaGroupAnnotation: vmeta.ReplicaGroupAValue,
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
	})

	It("should not allow malformed vertica version", func() {
		vdb := MakeVDB()
		vdb.Annotations[vmeta.VersionAnnotation] = "24.3.0"
		validateSpecValuesHaveErr(vdb, true)
		vdb.Annotations[vmeta.VersionAnnotation] = "v24.X.X"
		validateSpecValuesHaveErr(vdb, true)
		vdb.Annotations[vmeta.VersionAnnotation] = "v24.3.0-0"
		validateSpecValuesHaveErr(vdb, false)
	})

	It("should check subcluster immutability in sandbox", func() {
		newVdb := MakeVDB()
		mainClusterImageVer := "vertica-k8s:latest"
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "main", Type: PrimarySubcluster},
			{Name: "sc1", Type: SandboxPrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SandboxSecondarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster},
		}
		newVdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = SandboxSupportedMinVersion
		newVdb.ObjectMeta.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		resetStatusConditionsForDBInitialized(newVdb)
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))

		oldVdb := newVdb.DeepCopy()

		// cannot scale (out or in) any subcluster that is in a sandbox
		newVdb.Spec.Subclusters[1].Size = 2
		newVdb.Spec.Subclusters[3].Size = 4
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(2))

		// cannot remove a subcluster that is sandboxed
		// remove sc3 which is in sandbox2
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(2))

		// can remove a subcluster if it is removed
		// from any sandbox at the same time
		// remove sc3 which is also removed from sandbox2
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{{Name: "sc1", Type: PrimarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{{Name: "sc2", Type: PrimarySubcluster}}},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))

		// can remove a sandbox and all of its subclusters at the same time
		// remove sandbox1 and sc1 at the same time
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{{Name: "sc2", Type: PrimarySubcluster}}},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))

		// can remove an unsandboxed subcluster
		// remove sc4 which is not in a sandbox of oldVdb
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
	})

	It("should validate sandboxes", func() {
		vdb := MakeVDB()
		mainClusterImageVer := "vertica-k8s:latest"
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Size: 3, Type: PrimarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Size: 3, Type: SecondarySubcluster, ServiceType: v1.ServiceTypeClusterIP},
		}
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = SandboxSupportedMinVersion
		vdb.ObjectMeta.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		resetStatusConditionsForDBInitialized(vdb)
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(0))

		// cannot have empty sandbox name
		vdb.Spec.Sandboxes[0].Name = ""
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(2))

		// sandbox name must match rfc 1123 regex
		vdb.Spec.Sandboxes[0].Name = "-sandbox1"
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))

		// cannot have multiple sandboxes with the same name
		vdb.Spec.Sandboxes[0].Name = "sandbox2"
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))
		vdb.Spec.Sandboxes[0].Name = "sandbox1"

		// cannot have the image of a sandbox be different than the main cluster
		// when vertica is not in an upgrade and the sandbox has not been setup
		vdb.Spec.Sandboxes[1].Image = "vertica-k8s:v1"
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))
		// with empty string, sandbox will use the same image as main cluster
		vdb.Spec.Sandboxes[1].Image = ""
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(0))
		// when vertica is in an upgrade, we should not see an error
		vdb.Spec.Sandboxes[1].Image = "vertica-k8s:v1"
		resetStatusConditionsForUpgradeInProgress(vdb)
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(0))
		// after sandbox is setup, we should not see an error
		unsetStatusConditionsForUpgradeInProgress(vdb)
		vdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sandbox2", Subclusters: []string{"sc2", "sc3"}},
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(0))

		// cannot use on versions older than 24.3.0
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = "v23.0.0"
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = SandboxSupportedMinVersion

		// cannot use admintools deployments
		vdb.ObjectMeta.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))
		vdb.ObjectMeta.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue

		// Two errors:
		// 1. if sandbox subcluster type is not empty, it should be either primary or secondary
		// 2. there must be at least one primary subcluster in the sandbox sandbox1
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: "inalidType"}}},
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(2))

		// cannot have duplicate subclusters defined in a sandbox
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster}, {Name: "sc2", Type: SecondarySubcluster}}},
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))

		// cannot have a subcluster defined in multiple sandboxes
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}, {Name: "sc2", Type: SecondarySubcluster}}},
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))

		// should have at least one primary subcluster
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: SecondarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: SecondarySubcluster}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(2))

		// cannot have a non-existing subcluster defined in a sandbox
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}, {Name: "fake-sc", Type: SecondarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))

		// cannot have a primary subcluster defined in a sandbox
		// change sc1 from a secondary subcluster to a primary subcluster
		vdb.Spec.Subclusters[1].Type = PrimarySubcluster
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sandbox1", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc1", Type: PrimarySubcluster}}},
			{Name: "sandbox2", Image: mainClusterImageVer, Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(1))
	})

	It("should prevent the statefulset name from changing for existing subclusters", func() {
		newVdb := MakeVDB()
		oldVdb := newVdb.DeepCopy()
		newVdb.Spec.Subclusters[0].Annotations = map[string]string{
			vmeta.StsNameOverrideAnnotation: "change-sts-name",
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))

		// should allow it for new subclusters though
		newVdb.Spec.Subclusters[0].Annotations = oldVdb.Spec.Subclusters[0].Annotations
		newVdb.Spec.Subclusters = append(newVdb.Spec.Subclusters,
			Subcluster{
				Name: "new-name", Size: 1, Annotations: map[string]string{vmeta.StsNameOverrideAnnotation: "override-name"},
			},
		)
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
	})

	It("should prevent removing all of the primary subclusters from a sandbox", func() {
		oldVdb := MakeVDB()
		oldVdb.ObjectMeta.Annotations[vmeta.KSafetyAnnotation] = "0"
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 1},
			{Name: "sc2", Type: SecondarySubcluster, Size: 1},
			{Name: "sc3", Type: SecondarySubcluster, Size: 1},
			{Name: "sc4", Type: SecondarySubcluster, Size: 1},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster},
				{Name: "sc3", Type: PrimarySubcluster},
				{Name: "sc4", Type: SecondarySubcluster}}},
		}
		oldVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster, UpNodeCount: 1},
			{Name: "sc2", Type: SandboxPrimarySubcluster, UpNodeCount: 1},
			{Name: "sc3", Type: SandboxPrimarySubcluster, UpNodeCount: 1},
			{Name: "sc4", Type: SandboxSecondarySubcluster, UpNodeCount: 1},
		}
		newVdb := oldVdb.DeepCopy()

		// remove one of the primary subclusters
		newVdb.Spec.Sandboxes[0].Subclusters = []SandboxSubcluster{{Name: "sc2", Type: PrimarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster}}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
		// remove all of the primary subclusters
		newVdb.Spec.Sandboxes[0].Subclusters = []SandboxSubcluster{{Name: "sc4", Type: SecondarySubcluster}}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))
		newVdb.Spec.Sandboxes = nil
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
	})

	It("should allow a sandbox to have multiple primary subclusters", func() {
		vdb := MakeVDBForVclusterOps()
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = SandboxSupportedMinVersion
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2", Type: PrimarySubcluster}, {Name: "sc3", Type: PrimarySubcluster}}},
		}
		Ω(vdb.validateVerticaDBSpec()).Should(HaveLen(0))
	})

	It("should not allow sc type change if it's in a sandbox", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3},
			{Name: "sc2", Type: SecondarySubcluster, Size: 1},
			{Name: "sc3", Type: SecondarySubcluster, Size: 1},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{{Name: "sc3"}}},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.Subclusters[1].Type = PrimarySubcluster
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))

		newVdb.Spec.Subclusters[2].Type = PrimarySubcluster
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))
	})

	It("should disallow sandboxes size change during upgrade", func() {
		oldVdb := MakeVDB()
		oldVdb.Status.Conditions = []metav1.Condition{
			{Type: UpgradeInProgress, Status: metav1.ConditionTrue},
		}
		const sbName = "sb1"
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3},
			{Name: "sc2", Type: SecondarySubcluster, Size: 1},
			{Name: "sc3", Type: SecondarySubcluster, Size: 1},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1"},
			{Name: "sc2"},
			{Name: "sc3"},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: sbName, Subclusters: []SandboxSubcluster{{Name: "sc3"}}},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))
		newVdb.Annotations[vmeta.OnlineUpgradeSandboxAnnotation] = sbName
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
		newVdb.Spec.Sandboxes = nil
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: sbName, Subclusters: []SandboxSubcluster{{Name: "sc3"}}},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
	})

	It("should not allow to create a vdb with a shutdown sandbox", func() {
		vdb := MakeVDBForVclusterOps()
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = SandboxSupportedMinVersion
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3"}},
				Shutdown: true},
		}
		Ω(vdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		vdb.Spec.Sandboxes[0].Shutdown = false
		Ω(vdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
	})

	It("should not allow to create a vdb with a shutdown subcluster", func() {
		vdb := MakeVDBForVclusterOps()
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = SandboxSupportedMinVersion
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort, Shutdown: true},
		}
		vdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		Ω(vdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		vdb.Spec.Subclusters[2].Shutdown = false
		Ω(vdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		vdb.Spec.Subclusters[1].Shutdown = true
		Ω(vdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		vdb.Spec.Subclusters[1].Shutdown = false
		Ω(vdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
	})

	It("should not allow to add a subcluster whose Shutdown is true to a vdb", func() {
		newVdb := MakeVDB()
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc2", "sc3"}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
		}
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort, Shutdown: true}, // cause of error
		}
		Ω(newVdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Subclusters[3].Shutdown = false
		Ω(newVdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
	})

	It("should not allow to add a sanbox whose Shutdown is true to a vdb", func() {
		newVdb := MakeVDB()
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc1"}, {Name: "sc2", Type: SecondarySubcluster}}},
			{Name: "sand2", Subclusters: []SandboxSubcluster{
				{Name: "sc3"}}, Shutdown: true}, // cause of error
		}
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc1", "sc2"}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: SandboxPrimarySubcluster},
			{Name: "sc2", Type: SecondarySubcluster},
			{Name: "sc3", Type: SandboxPrimarySubcluster},
		}
		Ω(newVdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Sandboxes[1].Shutdown = false
		Ω(newVdb.checkNewSBoxOrSClusterShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
	})

	// When a cluster is annotated with \"vertica.com/shutdown-driven-by-sandbox\", its shutdown field will be immutable
	It("should not update a subcluster's shutdown field when its sandbox has shutdown set and the subcluster is annotated",
		func() {
			oldVdb := MakeVDB()
			oldVdb.Spec.Subclusters = []Subcluster{
				{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
				{Name: "sc2", Shutdown: true, Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP,
					Annotations: map[string]string{"vertica.com/shutdown-driven-by-sandbox": trueString}},
				{Name: "sc3", Shutdown: true, Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort,
					Annotations: map[string]string{"vertica.com/shutdown-driven-by-sandbox": trueString}},
			}
			oldVdb.Spec.Sandboxes = []Sandbox{
				{Name: "sand1", Shutdown: true, Subclusters: []SandboxSubcluster{
					{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
			}
			newVdb := oldVdb.DeepCopy()
			newVdb.Spec.Subclusters[2].Shutdown = false
			Ω(newVdb.checkSubclustersInShutdownSandbox(oldVdb, field.ErrorList{})).Should(HaveLen(1))
			newVdb.Spec.Subclusters[2].Shutdown = true
			Ω(newVdb.checkSubclustersInShutdownSandbox(oldVdb, field.ErrorList{})).Should(HaveLen(0))
			newVdb.Spec.Subclusters[1].Shutdown = false
			Ω(newVdb.checkSubclustersInShutdownSandbox(oldVdb, field.ErrorList{})).Should(HaveLen(1))
			newVdb.Spec.Subclusters[1].Shutdown = true
			Ω(newVdb.checkSubclustersInShutdownSandbox(oldVdb, field.ErrorList{})).Should(HaveLen(0))

		})

	It("should not unsandbox a subcluster when its shutdown field or its sandbox's shutdown field is set", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}, {Name: "sc4", Type: SecondarySubcluster}}},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc4", Type: SecondarySubcluster}}}, // to unsandbox sc3
		}
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc2", "sc3", "sc4"}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster},
		}
		oldVdb.Spec.Subclusters[2].Shutdown = true // cause of error
		// check subcluster shutdown in spec
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Subclusters[2].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[2].Shutdown = true
		// check subcluster shutdown in status
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[2].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		oldVdb.Spec.Sandboxes[0].Shutdown = true
		// check sandbox shutdown in spec
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Sandboxes[0].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))

		oldVdb.Spec.Subclusters[1].Shutdown = true
		oldVdb.Spec.Subclusters[2].Shutdown = true
		oldVdb.Spec.Subclusters[3].Shutdown = true
		oldVdb.Spec.Sandboxes[0].Shutdown = true
		newVdb = oldVdb.DeepCopy()
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Shutdown: true, Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc4", Type: SecondarySubcluster}}}, // to unsandbox sc3
		}
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Shutdown: true, Subclusters: []SandboxSubcluster{{Name: "sc2"},
				{Name: "sc3", Type: SecondarySubcluster}, {Name: "sc4", Type: SecondarySubcluster}}}, // to unsandbox sc3
		}
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		// to unsandbox sc3 and remove it
		oldVdb.Spec.Subclusters[1].Shutdown = false
		oldVdb.Spec.Subclusters[2].Shutdown = false
		oldVdb.Spec.Subclusters[3].Shutdown = false
		oldVdb.Spec.Sandboxes[0].Shutdown = false
		newVdb = oldVdb.DeepCopy()
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Shutdown: false, Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc4", Type: SecondarySubcluster}}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster},
		}
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		oldVdb.Spec.Sandboxes[0].Shutdown = true
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Sandboxes[0].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[2].Shutdown = true // sc3 status shutdown is set to true
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[2].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
	})

	It("should not unsandbox a sandbox when its shutdown field or its sandbox's shutdown field is set", func() {
		oldVdb := MakeVDB()
		// another unsandbox scenario where a sandbox in old vdb is unsandboxed in the new vdb
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
			{Name: "sand2", Subclusters: []SandboxSubcluster{
				{Name: "sc4"}}},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}}, // to unsandbox sc4
		}
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc2", "sc3"}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster},
		}
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		oldVdb.Spec.Sandboxes[1].Shutdown = true
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Sandboxes[1].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		oldVdb.Spec.Subclusters[3].Shutdown = true
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Subclusters[3].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[3].Shutdown = true
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[3].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
	})

	It("should not unsandbox a subcluster when its shutdown field or its sandbox's shutdown field is set", func() {
		// another scenario where one subcluster is moved from one sandbox to another
		oldVdb := MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
			{Name: "sand2", Subclusters: []SandboxSubcluster{
				{Name: "sc4"}}},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}}}, // sc3 moved to sand2
			{Name: "sand2", Subclusters: []SandboxSubcluster{
				{Name: "sc3"}, {Name: "sc4", Type: SecondarySubcluster}}},
		}
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc2"}},
			{Name: "sand2", Subclusters: []string{"sc3", "sc4"}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SandboxPrimarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster},
		}
		oldVdb.Spec.Sandboxes[0].Shutdown = true
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Sandboxes[0].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		oldVdb.Spec.Subclusters[2].Shutdown = true
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Subclusters[2].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[2].Shutdown = true
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[2].Shutdown = false
		Ω(newVdb.checkUnsandboxShutdownConditions(oldVdb, field.ErrorList{})).Should(HaveLen(0))

	})

	It("should not scale out/in a subcluster when its shutdown field or its sandbox's shutdown field is set", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{{Name: "sc1"}}},
			{Name: "sand2", Subclusters: []SandboxSubcluster{{Name: "sc2"}}},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc1"}},
			{Name: "sand2", Subclusters: []string{"sc2"}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "main", Type: PrimarySubcluster},
			{Name: "sc1", Type: SandboxPrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
		}
		newVdb.Spec.Subclusters[3].Size = 4
		Ω(newVdb.checkShutdownForScaleOutOrIn(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Subclusters[3].Shutdown = true
		Ω(newVdb.checkShutdownForScaleOutOrIn(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Subclusters[3].Size = 2
		Ω(newVdb.checkShutdownForScaleOutOrIn(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Subclusters[3].Shutdown = false
		Ω(newVdb.checkShutdownForScaleOutOrIn(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[3].Shutdown = true
		Ω(newVdb.checkShutdownForScaleOutOrIn(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[3].Shutdown = false
		Ω(newVdb.checkShutdownForScaleOutOrIn(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Subclusters[1].Size = 4
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))

	})

	It("should not sandbox a subcluster when its shutdown field or its sandbox's shutdown field is set", func() {
		newVdb := MakeVDB()
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		// to sandbox sc3 in sand2. sc3 was existing previously but not in a sandbox
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc1"}}},
			{Name: "sand2", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc1"}},
			{Name: "sand2", Subclusters: []string{"sc2"}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "main", Type: PrimarySubcluster},
			{Name: "sc1", Type: SandboxPrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
		}
		newVdb.Spec.Subclusters[3].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Subclusters[3].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[3].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[3].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Sandboxes[1].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Sandboxes[1].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))

		// sc3 not found in status and to be added
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "main", Type: PrimarySubcluster},
			{Name: "sc1", Type: SandboxPrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
		}
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Sandboxes[1].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Sandboxes[1].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))

		// sc3 is to be unsandboxsed from sand1 and sandboxed in sand2
		oldVdb := MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc1"}, {Name: "sc3", Type: SecondarySubcluster}}},
			{Name: "sand2", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}}},
		}
		newVdb = oldVdb.DeepCopy()
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "main", Type: PrimarySubcluster},
			{Name: "sc1", Type: SandboxPrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
		}
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc1", "sc3"}},
			{Name: "sand2", Subclusters: []string{"sc2"}},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc1"}}},
			{Name: "sand2", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Subclusters[3].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Subclusters[3].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[3].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[3].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Sandboxes[1].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Sandboxes[1].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
	})

	It("should not sandbox a subcluster when sandbox/ other subclusters has shutdown set", func() {
		// another scenario where one subcluster is moved from one sandbox to another
		newVdb := MakeVDB()
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "main", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc1", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		// to sandbox sc3 in sand2. sc3 was existing previously but not in a sandbox
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{
				{Name: "sc1"}}},
			{Name: "sand2", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc1"}},
			{Name: "sand2", Subclusters: []string{"sc2"}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "main", Type: PrimarySubcluster},
			{Name: "sc1", Type: SandboxPrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
		}
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Sandboxes[1].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Sandboxes[1].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Subclusters[2].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Subclusters[2].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[2].Shutdown = true
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[2].Shutdown = false
		Ω(newVdb.checkSClusterToBeSandboxedShutdownUnset(field.ErrorList{})).Should(HaveLen(0))

	})

	It("should not change image for a sandbox if shutdown is set for it or its subcluster in either spec or status", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Image: "vertica-k8s:v1", Subclusters: []SandboxSubcluster{{Name: "sc2"},
				{Name: "sc3", Type: SecondarySubcluster}, {Name: "sc4", Type: SecondarySubcluster}}},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Subclusters: []SandboxSubcluster{{Name: "sc2"},
				{Name: "sc3", Type: SecondarySubcluster}, {Name: "sc4", Type: SecondarySubcluster}}},
		}

		newVdb.Status.Sandboxes = []SandboxStatus{
			{Name: "sand1", Subclusters: []string{"sc2", "sc3", "sc4"}},
		}

		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster},
			{Name: "sc2", Type: SandboxPrimarySubcluster},
			{Name: "sc3", Type: SecondarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster},
		}
		newVdb.Spec.Sandboxes[0].Shutdown = true
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Sandboxes[0].Shutdown = false
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Subclusters[2].Shutdown = true
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Subclusters[2].Shutdown = false
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Subclusters[3].Shutdown = true
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Spec.Subclusters[3].Shutdown = false
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[2].Shutdown = true
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[2].Shutdown = false
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[3].Shutdown = true
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		newVdb.Status.Subclusters[3].Shutdown = false
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Status.Subclusters[0].Shutdown = true
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Sandboxes[0].Shutdown = false
		Ω(newVdb.checkShutdownSandboxImage(oldVdb, field.ErrorList{})).Should(HaveLen(0))

	})

	It("should not terminate a sandbox whose shutdown field is set", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Image: "vertica-k8s:v1", Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.Sandboxes = []Sandbox{} // sandbox and its subclusters are gone
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes[0].Shutdown = true
		Ω(newVdb.checkShutdownForSandboxesToBeRemoved(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Sandboxes[0].Shutdown = false
		Ω(newVdb.checkShutdownForSandboxesToBeRemoved(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		newVdb.Spec.Subclusters = []Subcluster{ // unsandbox and subclusters persist
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		Ω(newVdb.checkShutdownForSandboxesToBeRemoved(oldVdb, field.ErrorList{})).Should(HaveLen(0))
		oldVdb.Spec.Sandboxes[0].Shutdown = true
		Ω(newVdb.checkShutdownForSandboxesToBeRemoved(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb.Spec.Sandboxes[0].Shutdown = false
		Ω(newVdb.checkShutdownForSandboxesToBeRemoved(oldVdb, field.ErrorList{})).Should(HaveLen(0))

	})

	It("should not remove a subcluster whose shutdown field in spec/status is set", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Shutdown: true, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Shutdown: true, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Image: "vertica-k8s:v1", Shutdown: true, Subclusters: []SandboxSubcluster{
				{Name: "sc2"}, {Name: "sc3", Type: SecondarySubcluster}}},
		}
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.Subclusters = []Subcluster{ // sc3 is removed from sandbox and vdb
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Shutdown: true, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Image: "vertica-k8s:v1", Shutdown: true, Subclusters: []SandboxSubcluster{{Name: "sc2"}}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster},
			{Name: "sc2", Shutdown: true, Type: SandboxPrimarySubcluster},
			{Name: "sc3", Shutdown: true, Type: SecondarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster},
		}

		Ω(newVdb.checkShutdownForSubclustersToBeRemoved(oldVdb, field.ErrorList{})).Should(HaveLen(1))
		oldVdb = MakeVDB()
		oldVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Shutdown: true, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc3", Type: SecondarySubcluster, Shutdown: true, Size: 3, ServiceType: v1.ServiceTypeNodePort},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		oldVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Shutdown: true, Subclusters: []SandboxSubcluster{{Name: "sc2"}}},
		}
		newVdb = oldVdb.DeepCopy()
		newVdb.Spec.Subclusters = []Subcluster{ // sc3 is removed from vdb
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Shutdown: true, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc4", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeNodePort},
		}
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand1", Shutdown: true, Subclusters: []SandboxSubcluster{{Name: "sc2"}}},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Type: PrimarySubcluster},
			{Name: "sc2", Shutdown: true, Type: SandboxPrimarySubcluster},
			{Name: "sc3", Shutdown: true, Type: SecondarySubcluster},
			{Name: "sc4", Type: SecondarySubcluster},
		}
		Ω(newVdb.checkShutdownForSubclustersToBeRemoved(oldVdb, field.ErrorList{})).Should(HaveLen(1))
	})

	It("should not accept invalid client server tls modes", func() {
		newVdb := MakeVDB()
		SetVDBForTLS(newVdb)
		newVdb.Spec.ClientServerTLS.Mode = "TRY_VERIFY"
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))
		newVdb.Spec.ClientServerTLS.Mode = "try_verify"
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))
		newVdb.Spec.ClientServerTLS.Mode = "try_VERIFY"
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))
		newVdb.Spec.ClientServerTLS.Mode = "disable"
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))
		newVdb.Spec.ClientServerTLS.Mode = "Enable"
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))
		newVdb.Spec.ClientServerTLS.Mode = "VERIFY_CA"
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))
		newVdb.Spec.ClientServerTLS.Mode = "VERIFY_FULL"
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))
		newVdb.Spec.ClientServerTLS.Mode = "VERIFYCA"
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(1))
		newVdb.Spec.ClientServerTLS.Mode = ""
		Ω(newVdb.validateVerticaDBSpec()).Should(HaveLen(0))
	})

	It("should forbid changes when TLS config update is in progress", func() {
		oldVdb := MakeVDBForCertRotationEnabled()
		oldVdb.Status.Conditions = append(oldVdb.Status.Conditions, metav1.Condition{
			Type:   TLSConfigUpdateInProgress,
			Status: metav1.ConditionTrue,
		})
		newVdb := oldVdb.DeepCopy()
		// Only TLS config fields changed: allowed
		newVdb.Spec.HTTPSNMATLS.Secret = newSecret
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())

		// Other field changed: forbidden
		newVdb = oldVdb.DeepCopy()
		newVdb.Spec.Image = "vertica:latest"
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
		Ω(allErrs[0].Error()).Should(ContainSubstring("no changes allowed while TLS config update is in progress"))
	})

	It("should not allow disabling mutual TLS after it's enabled", func() {
		oldVdb := MakeVDB()
		oldVdb.Annotations[vmeta.EnableTLSAuthAnnotation] = "true"
		newVdb := oldVdb.DeepCopy()
		newVdb.Annotations[vmeta.EnableTLSAuthAnnotation] = falseString
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
		Ω(allErrs[0].Error()).Should(ContainSubstring("cannot disable mutual TLS after it's enabled"))
	})

	It("should call checkDisallowedMutualTLSChanges when mutual TLS is not enabled", func() {
		oldVdb := MakeVDB()
		oldVdb.Annotations[vmeta.EnableTLSAuthAnnotation] = falseString
		oldVdb.Spec.HTTPSNMATLS.Secret = oldSecret
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.HTTPSNMATLS.Secret = "changed"
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
	})

	It("should not allow changing https secret while enabling mutual", func() {
		oldVdb := MakeVDB()
		oldVdb.Annotations[vmeta.EnableTLSAuthAnnotation] = falseString
		oldVdb.Spec.HTTPSNMATLS.Secret = oldSecret

		newVdb := oldVdb.DeepCopy()

		newVdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())

		newVdb.Spec.HTTPSNMATLS.Secret = "changed"
		allErrs = newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
	})

	It("should not allow changing nmaTLSSecret", func() {
		oldVdb := MakeVDB()
		oldVdb.Spec.NMATLSSecret = "old-nma"
		newVdb := oldVdb.DeepCopy()
		newVdb.Spec.NMATLSSecret = "new-nma"
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).ShouldNot(BeEmpty())
		Ω(allErrs[0].Error()).Should(ContainSubstring("nmaTLSSecret cannot be changed"))
	})

	It("should allow no errors when nothing changes", func() {
		oldVdb := MakeVDB()
		newVdb := oldVdb.DeepCopy()
		allErrs := newVdb.checkValidTLSConfigUpdate(oldVdb, nil)
		Ω(allErrs).Should(BeEmpty())
	})

	It("should return error if both TLS and NMA certs mount are enabled", func() {
		vdb := MakeVDB()
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = trueString
		allErrs := vdb.hasNoConflictbetweenTLSAndCertMount(field.ErrorList{})
		Expect(allErrs).ShouldNot(BeEmpty())
		Expect(allErrs[0].Error()).To(ContainSubstring("cannot set enable-tls-auth and mount-nma-certs to true at the same time"))
	})

	It("should not return error if only TLS is enabled", func() {
		vdb := MakeVDB()
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		delete(vdb.Annotations, vmeta.MountNMACertsAnnotation)
		allErrs := vdb.hasNoConflictbetweenTLSAndCertMount(field.ErrorList{})
		Expect(allErrs).Should(BeEmpty())
	})

	It("should not return error if only NMA certs mount is enabled", func() {
		vdb := MakeVDB()
		delete(vdb.Annotations, vmeta.EnableTLSAuthAnnotation)
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = trueString
		allErrs := vdb.hasNoConflictbetweenTLSAndCertMount(field.ErrorList{})
		Expect(allErrs).Should(BeEmpty())
	})

	It("should not return error if neither TLS nor NMA certs mount is enabled", func() {
		vdb := MakeVDB()
		delete(vdb.Annotations, vmeta.EnableTLSAuthAnnotation)
		delete(vdb.Annotations, vmeta.MountNMACertsAnnotation)
		allErrs := vdb.hasNoConflictbetweenTLSAndCertMount(field.ErrorList{})
		Expect(allErrs).Should(BeEmpty())
	})

	It("should return no error if nothing changes", func() {
		allErrs := newVdb1.checkImmutableTLSConfig(oldVdb1, nil)
		Expect(allErrs).Should(BeEmpty())
	})

	It("should return error if httpsNMATLS is changed during TLS config update in progress and does not match status", func() {
		newVdb1.Spec.HTTPSNMATLS.Secret = newSecret
		newVdb1.Status.Conditions = append(newVdb1.Status.Conditions, *MakeCondition(TLSConfigUpdateInProgress, metav1.ConditionTrue, ""))
		allErrs := newVdb1.checkImmutableTLSConfig(oldVdb1, nil)
		Expect(allErrs).Should(HaveLen(1))
		Expect(allErrs[0].Error()).To(ContainSubstring("httpsNMATLS cannot be changed when tls config update is in progress"))
	})

	It("should not return error if httpsNMATLS is changed during TLS config update in progress but matches status", func() {
		newVdb1.Spec.HTTPSNMATLS.Secret = newSecret
		newVdb1.Status.TLSConfigs[0].Secret = newSecret
		newVdb1.Status.Conditions = append(newVdb1.Status.Conditions, *MakeCondition(TLSConfigUpdateInProgress, metav1.ConditionTrue, ""))
		allErrs := newVdb1.checkImmutableTLSConfig(oldVdb1, nil)
		Expect(allErrs).Should(BeEmpty())
	})

	It("should return error if clientServerTLS is changed during TLS config update in progress and does not match status", func() {
		newVdb1.Spec.ClientServerTLS.Secret = newSecret
		newVdb1.Status.Conditions = append(newVdb1.Status.Conditions, *MakeCondition(TLSConfigUpdateInProgress, metav1.ConditionTrue, ""))
		allErrs := newVdb1.checkImmutableTLSConfig(oldVdb1, nil)
		Expect(allErrs).Should(HaveLen(1))
		Expect(allErrs[0].Error()).To(ContainSubstring("clientServerTLS cannot be changed when tls config update is in progress"))
	})

	It("should not return error if clientServerTLS is changed during TLS config update in progress but matches status", func() {
		newVdb1.Spec.ClientServerTLS.Secret = newSecret
		newVdb1.Status.TLSConfigs[1].Secret = newSecret
		newVdb1.Status.Conditions = append(newVdb1.Status.Conditions, *MakeCondition(TLSConfigUpdateInProgress, metav1.ConditionTrue, ""))
		allErrs := newVdb1.checkImmutableTLSConfig(oldVdb1, nil)
		Expect(allErrs).Should(BeEmpty())
	})

	It("should return no error if initPolicy is not Revive", func() {
		vdb := MakeVDB()
		vdb.Spec.InitPolicy = CommunalInitPolicyCreate
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		vdb.Spec.HTTPSNMATLS.Secret = ""
		vdb.Spec.ClientServerTLS.Secret = ""
		allErrs := vdb.hasTLSSecretsSetForRevive(field.ErrorList{})
		Expect(allErrs).Should(BeEmpty())
	})

	It("should return no error if TLS is not enabled", func() {
		vdb := MakeVDB()
		vdb.Spec.InitPolicy = CommunalInitPolicyRevive
		delete(vdb.Annotations, vmeta.EnableTLSAuthAnnotation)
		vdb.Spec.HTTPSNMATLS.Secret = ""
		vdb.Spec.ClientServerTLS.Secret = ""
		allErrs := vdb.hasTLSSecretsSetForRevive(field.ErrorList{})
		Expect(allErrs).Should(BeEmpty())
	})

	It("should return error if HTTPSNMATLS.Secret is empty when TLS is enabled and initPolicy is Revive", func() {
		vdb := MakeVDB()
		vdb.Spec.InitPolicy = CommunalInitPolicyRevive
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		vdb.Spec.HTTPSNMATLS.Secret = ""
		vdb.Spec.ClientServerTLS.Secret = "client-secret"
		allErrs := vdb.hasTLSSecretsSetForRevive(field.ErrorList{})
		Expect(allErrs).Should(HaveLen(1))
		Expect(allErrs[0].Field).To(ContainSubstring("spec.httpsNMATLS.secret"))
		Expect(allErrs[0].Error()).To(ContainSubstring("httpsNMATLS.Secret cannot be empty"))
	})

	It("should return error if ClientServerTLS.Secret is empty when TLS is enabled and initPolicy is Revive", func() {
		vdb := MakeVDB()
		vdb.Spec.InitPolicy = CommunalInitPolicyRevive
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		vdb.Spec.HTTPSNMATLS.Secret = newSecret
		vdb.Spec.ClientServerTLS.Secret = ""
		allErrs := vdb.hasTLSSecretsSetForRevive(field.ErrorList{})
		Expect(allErrs).Should(HaveLen(1))
		Expect(allErrs[0].Field).To(ContainSubstring("spec.clientServerTLS.secret"))
		Expect(allErrs[0].Error()).To(ContainSubstring("clientServerTLS.Secret cannot be empty"))
	})

	It("should return errors for both secrets if both are empty", func() {
		vdb := MakeVDB()
		vdb.Spec.InitPolicy = CommunalInitPolicyRevive
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		vdb.Spec.HTTPSNMATLS.Secret = ""
		vdb.Spec.ClientServerTLS.Secret = ""
		allErrs := vdb.hasTLSSecretsSetForRevive(field.ErrorList{})
		Expect(allErrs).Should(HaveLen(2))
		Expect(allErrs[0].Field).To(ContainSubstring("spec.httpsNMATLS.secret"))
		Expect(allErrs[1].Field).To(ContainSubstring("spec.clientServerTLS.secret"))
	})

	It("should return no error if both secrets are set and TLS is enabled and initPolicy is Revive", func() {
		vdb := MakeVDB()
		vdb.Spec.InitPolicy = CommunalInitPolicyRevive
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		vdb.Spec.HTTPSNMATLS.Secret = newSecret
		vdb.Spec.ClientServerTLS.Secret = newSecret
		allErrs := vdb.hasTLSSecretsSetForRevive(field.ErrorList{})
		Expect(allErrs).Should(BeEmpty())
	})

	It("should not allow tls config to change when an operation is in progress", func() {
		newVdb := MakeVDB()
		dbInitCond := metav1.Condition{
			Type: DBInitialized, Status: metav1.ConditionTrue, Reason: "DBInitialized",
		}
		const testHTTPSSecret = "test-https-secret" // #nosec G101
		const testClientServerSecret = "test-client-server-secret"
		const verifyCa = "VERIFY_CA"
		const tryVerify = "TRY_VERIFY"
		newVdb.Annotations[vmeta.VersionAnnotation] = TLSAuthMinVersion
		newVdb.Annotations[vmeta.VClusterOpsAnnotation] = trueString
		newVdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		newVdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", Type: PrimarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
			{Name: "sc2", Type: SecondarySubcluster, Size: 3, ServiceType: v1.ServiceTypeClusterIP},
		}
		newVdb.Status.Subclusters = []SubclusterStatus{
			{Name: "sc1", Shutdown: false, AddedToDBCount: 3, UpNodeCount: 3, Type: PrimarySubcluster},
			{Name: "sc2", Shutdown: false, AddedToDBCount: 3, UpNodeCount: 3, Type: SecondarySubcluster},
		}
		newVdb.Spec.HTTPSNMATLS.Mode = tryVerify
		newVdb.Spec.ClientServerTLS.Mode = tryVerify
		newVdb.Spec.HTTPSNMATLS.Secret = testHTTPSSecret
		newVdb.Spec.ClientServerTLS.Secret = testClientServerSecret
		oldVdb := newVdb.DeepCopy()
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))

		// when upgrade is in progress, we cannot modify the tls config
		newVdb.Status.Conditions = []metav1.Condition{
			dbInitCond,
			{Type: UpgradeInProgress, Status: metav1.ConditionTrue, Reason: "UpgradeStarted"},
		}
		newVdb.Spec.HTTPSNMATLS.Mode = verifyCa
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))
		newVdb.Status.Conditions = []metav1.Condition{
			dbInitCond,
			{Type: UpgradeInProgress, Status: metav1.ConditionFalse, Reason: "UpgradeStarted"},
		}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
		newVdb.Spec.HTTPSNMATLS.Mode = tryVerify
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))

		// when subcluster shutdown is in progress, we cannot modify the tls config
		newVdb.Spec.Subclusters[0].Shutdown = true
		newVdb.Spec.HTTPSNMATLS.Secret = "test-https-secret-1"
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))
		newVdb.Spec.HTTPSNMATLS.Secret = testHTTPSSecret
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
		newVdb.Spec.Subclusters[0].Shutdown = false
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))

		// when subcluster size is changed, we cannot modify the tls config
		newVdb.Spec.Subclusters[0].Size = 4
		newVdb.Spec.ClientServerTLS.Secret = "test-client-server-secret-1"
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))
		newVdb.Spec.ClientServerTLS.Secret = testClientServerSecret
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
		newVdb.Spec.Subclusters[0].Size = 3
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))

		// we cannot rotate certs when there are sandboxes
		newVdb.Spec.Sandboxes = []Sandbox{
			{Name: "sand", Subclusters: []SandboxSubcluster{{Name: newVdb.Spec.Subclusters[1].Name}}},
		}
		newVdb.Spec.ClientServerTLS.Mode = verifyCa
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))
		newVdb.Spec.Sandboxes = []Sandbox{}
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
		newVdb.Spec.ClientServerTLS.Mode = tryVerify
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))

		// tls auth cannot be disabled
		newVdb.Annotations[vmeta.EnableTLSAuthAnnotation] = falseString
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(1))
		newVdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueString
		Ω(newVdb.validateImmutableFields(oldVdb)).Should(HaveLen(0))
	})

	It("should return no errors when both TLS configs are nil", func() {
		vdb := MakeVDBForTLS()
		vdb.Spec.HTTPSNMATLS = nil
		vdb.Spec.ClientServerTLS = nil
		allErrs := vdb.hasValidTLSModes(field.ErrorList{})
		Expect(allErrs).To(BeEmpty())
	})

	It("should return no errors for valid HTTPSNMATLS mode", func() {
		vdb := MakeVDBForTLS()
		vdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "verify_ca"}
		vdb.Spec.ClientServerTLS = nil
		allErrs := vdb.hasValidTLSModes(field.ErrorList{})
		Expect(allErrs).To(BeEmpty())
	})

	It("should return no errors for valid ClientServerTLS mode", func() {
		vdb := MakeVDBForTLS()
		vdb.Spec.HTTPSNMATLS = nil
		vdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "verify_full"}
		allErrs := vdb.hasValidTLSModes(field.ErrorList{})
		Expect(allErrs).To(BeEmpty())
	})

	It("should return errors for invalid HTTPSNMATLS mode", func() {
		vdb := MakeVDBForTLS()
		vdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "invalid_mode"}
		vdb.Spec.ClientServerTLS = nil
		allErrs := vdb.hasValidTLSModes(field.ErrorList{})
		Expect(allErrs).ToNot(BeEmpty())
	})

	It("should return errors for invalid ClientServerTLS mode", func() {
		vdb := MakeVDBForTLS()
		vdb.Spec.HTTPSNMATLS = nil
		vdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "bad_mode"}
		allErrs := vdb.hasValidTLSModes(field.ErrorList{})
		Expect(allErrs).ToNot(BeEmpty())
	})

	It("should return errors for both invalid modes", func() {
		vdb := MakeVDBForTLS()
		vdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "foo"}
		vdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "bar"}
		allErrs := vdb.hasValidTLSModes(field.ErrorList{})
		Expect(allErrs).To(HaveLen(2))
	})

	It("should return error if httpsNMATLS mode changes only in case", func() {
		oldVdb := MakeVDBForTLS()
		newVdb := oldVdb.DeepCopy()
		oldVdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "verify_ca"}
		newVdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "VERIFY_CA"}
		// Modes differ in case, but normalized value is the same
		allErrs := newVdb.checkTLSModeCaseInsensitiveChange(oldVdb, field.ErrorList{})
		Expect(allErrs).To(HaveLen(1))
		Expect(allErrs[0].Error()).To(ContainSubstring("case insensitive mode change is not allowed for httpsNMATLS"))
	})

	It("should return error if clientServerTLS mode changes only in case", func() {
		oldVdb := MakeVDBForTLS()
		newVdb := oldVdb.DeepCopy()
		oldVdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "verify_full"}
		newVdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "VERIFY_FULL"}
		allErrs := newVdb.checkTLSModeCaseInsensitiveChange(oldVdb, field.ErrorList{})
		Expect(allErrs).To(HaveLen(1))
		Expect(allErrs[0].Error()).To(ContainSubstring("case insensitive mode change is not allowed for clientServerTLS"))
	})

	It("should return errors for both httpsNMATLS and clientServerTLS if both change only in case", func() {
		oldVdb := MakeVDBForTLS()
		newVdb := oldVdb.DeepCopy()
		oldVdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "try_verify"}
		newVdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "TRY_VERIFY"}
		oldVdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "enable"}
		newVdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "ENABLE"}
		allErrs := newVdb.checkTLSModeCaseInsensitiveChange(oldVdb, field.ErrorList{})
		Expect(allErrs).To(HaveLen(2))
		Expect(allErrs[0].Error()).To(ContainSubstring("case insensitive mode change is not allowed for httpsNMATLS"))
		Expect(allErrs[1].Error()).To(ContainSubstring("case insensitive mode change is not allowed for clientServerTLS"))
	})

	It("should not return error if httpsNMATLS mode changes to a different value", func() {
		oldVdb := MakeVDBForTLS()
		newVdb := oldVdb.DeepCopy()
		oldVdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "verify_ca"}
		newVdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "verify_full"}
		allErrs := newVdb.checkTLSModeCaseInsensitiveChange(oldVdb, field.ErrorList{})
		Expect(allErrs).To(BeEmpty())
	})

	It("should not return error if clientServerTLS mode changes to a different value", func() {
		oldVdb := MakeVDBForTLS()
		newVdb := oldVdb.DeepCopy()
		oldVdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "try_verify"}
		newVdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "verify_ca"}
		allErrs := newVdb.checkTLSModeCaseInsensitiveChange(oldVdb, field.ErrorList{})
		Expect(allErrs).To(BeEmpty())
	})

	It("should not return error if modes are unchanged", func() {
		oldVdb := MakeVDBForTLS()
		newVdb := oldVdb.DeepCopy()
		oldVdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "verify_ca"}
		newVdb.Spec.HTTPSNMATLS = &TLSConfigSpec{Mode: "verify_ca"}
		oldVdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "enable"}
		newVdb.Spec.ClientServerTLS = &TLSConfigSpec{Mode: "enable"}
		allErrs := newVdb.checkTLSModeCaseInsensitiveChange(oldVdb, field.ErrorList{})
		Expect(allErrs).To(BeEmpty())
	})
})

func createVDBHelper() *VerticaDB {
	vdb := MakeVDB()
	// check other field values in the MakeVDB function
	sc := &vdb.Spec.Subclusters[0]
	sc.Type = PrimarySubcluster
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

func validateImmutableFields(vdbUpdate *VerticaDB, expectError bool) {
	vdb := createVDBHelper()
	checkErrorsForImmutableFields(vdb, vdbUpdate, expectError)
}

func checkErrorsForImmutableFields(vdbOrig, vdbUpdate *VerticaDB, expectError bool) {
	allErrs := vdbUpdate.validateImmutableFields(vdbOrig)
	if expectError {
		Expect(allErrs).ShouldNot(BeNil())
	} else {
		Expect(allErrs).Should(BeNil())
	}
}

func resetStatusConditionsForUpgradeInProgress(v *VerticaDB) {
	resetStatusConditionsForCondition(v, UpgradeInProgress, metav1.ConditionTrue)
}

func unsetStatusConditionsForUpgradeInProgress(v *VerticaDB) {
	resetStatusConditionsForCondition(v, UpgradeInProgress, metav1.ConditionFalse)
}

func resetStatusConditionsForDBInitialized(v *VerticaDB) {
	resetStatusConditionsForCondition(v, DBInitialized, metav1.ConditionTrue)
}

func resetStatusConditionsForCertRotationInProgress(v *VerticaDB) {
	resetStatusConditionsForCondition(v, TLSConfigUpdateInProgress, metav1.ConditionTrue)
}

func resetStatusConditionsForCondition(v *VerticaDB, conditionType string, status metav1.ConditionStatus) {
	v.Status.Conditions = make([]metav1.Condition, 0)
	cond := MakeCondition(conditionType, status, "")
	meta.SetStatusCondition(&v.Status.Conditions, *cond)
}

func setOnlineUpgradeInProgress(v *VerticaDB) {
	v.Status.Conditions = append(v.Status.Conditions, *MakeCondition(OnlineUpgradeInProgress, metav1.ConditionTrue, "started"))
}
