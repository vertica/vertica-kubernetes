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

//nolint:lll
package v1

import (
	"fmt"
	"reflect"
	"strings"

	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	invalidDBNameChars       = "$=<>`" + `'^\".@*?#&/-:;{}()[] \~!%+|,`
	dbNameLengthLimit        = 30
	KSafety0MinHosts         = 1
	KSafety0MaxHosts         = 3
	KSafety1MinHosts         = 3
	portLowerBound           = 30000
	portUpperBound           = 32767
	LocalDataPVC             = "local-data"
	PodInfoMountName         = "podinfo"
	LicensingMountName       = "licensing"
	HadoopConfigMountName    = "hadoop-conf"
	Krb5SecretMountName      = "krb5"
	SSHMountName             = "ssh"
	HTTPServerCertsMountName = "http-server-certs"
	DepotMountName           = "depot"
	S3Prefix                 = "s3://"
	GCloudPrefix             = "gs://"
	AzurePrefix              = "azb://"
)

// hdfsPrefixes are prefixes for an HDFS path.
var hdfsPrefixes = []string{"webhdfs://", "swebhdfs://"}

// log is for logging in this package.
var verticadblog = logf.Log.WithName("verticadb-resource")

func (v *VerticaDB) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(v).
		Complete()
}

// IsHDFS returns true if the communal path is stored in an HDFS path
func (v *VerticaDB) IsHDFS() bool {
	for _, p := range hdfsPrefixes {
		if strings.HasPrefix(v.Spec.Communal.Path, p) {
			return true
		}
	}
	return false
}

// IsS3 returns true if VerticaDB has a communal path for S3 compatible storage.
func (v *VerticaDB) IsS3() bool {
	return strings.HasPrefix(v.Spec.Communal.Path, S3Prefix)
}

// ISGCloud returns true if VerticaDB has a communal path in Google Cloud Storage
func (v *VerticaDB) IsGCloud() bool {
	return strings.HasPrefix(v.Spec.Communal.Path, GCloudPrefix)
}

// IsAzure returns true if VerticaDB has a communal path in Azure Blob Storage
func (v *VerticaDB) IsAzure() bool {
	return strings.HasPrefix(v.Spec.Communal.Path, AzurePrefix)
}

// IsSseS3 returns true if VerticaDB is setup for S3 SSE-S3 server-side encryption
func (v *VerticaDB) IsSseS3() bool {
	return strings.EqualFold(string(v.Spec.Communal.S3ServerSideEncryption), string(SseS3))
}

// IsSseKMS returns true if VerticaDB is setup for S3 SSE-KMS server-side encryption
func (v *VerticaDB) IsSseKMS() bool {
	return strings.EqualFold(string(v.Spec.Communal.S3ServerSideEncryption), string(SseKMS))
}

// IsSseC returns true if VerticaDB is setup for S3 SSE-C server-side encryption
func (v *VerticaDB) IsSseC() bool {
	return strings.EqualFold(string(v.Spec.Communal.S3ServerSideEncryption), string(SseC))
}

// IsKnownSseType returns true if VerticaDB is setup for S3 server-side encryption
func (v *VerticaDB) IsKnownSseType() bool {
	if v.IsSseS3() || v.IsSseKMS() || v.IsSseC() {
		return true
	}
	return false
}

// IsKnownCommunalPrefix returns true if the communal has a known prefix that
// indicates the type of communal storage. False means the communal path was
// empty or is a POSIX path.
func (v *VerticaDB) IsKnownCommunalPrefix() bool {
	if v.IsHDFS() || v.IsS3() || v.IsGCloud() || v.IsAzure() {
		return true
	}
	return false
}

// HasKerberosConfig returns true if VerticaDB is setup for Kerberos authentication.
func (v *VerticaDB) HasKerberosConfig() bool {
	// We have a webhook check that makes sure if the principal is set, the
	// other things are set too.
	return v.Spec.Communal.KerberosServiceName != ""
}

//+kubebuilder:webhook:path=/mutate-vertica-com-v1beta1-verticadb,mutating=true,failurePolicy=fail,sideEffects=None,groups=vertica.com,resources=verticadbs,verbs=create;update,versions=v1beta1,name=mverticadb.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &VerticaDB{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (v *VerticaDB) Default() {
	verticadblog.Info("default", "name", v.Name, "GroupVersion", GroupVersion)

	// imagePullPolicy: if not set should default to Always if the tag in the image is latest,
	// otherwise it should be IfNotPresent (set in verticadb_types.go)
	if strings.HasSuffix(v.Spec.Image, ":latest") {
		v.Spec.ImagePullPolicy = v1.PullAlways
	}
	if v.Spec.Communal.Region == "" && v.IsS3() {
		v.Spec.Communal.Region = DefaultS3Region
	}
	if v.Spec.Communal.Region == "" && v.IsGCloud() {
		v.Spec.Communal.Region = DefaultGCloudRegion
	}
	// Default the endpoint for google cloud if not specified
	if v.Spec.Communal.Endpoint == "" && v.IsGCloud() {
		v.Spec.Communal.Endpoint = DefaultGCloudEndpoint
	}
	v.Spec.TemporarySubclusterRouting.Template.IsPrimary = false
	v.setDefaultServiceName()
}

//+kubebuilder:webhook:path=/validate-vertica-com-v1beta1-verticadb,mutating=false,failurePolicy=fail,sideEffects=None,groups=vertica.com,resources=verticadbs,verbs=create;update,versions=v1beta1,name=vverticadb.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &VerticaDB{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaDB) ValidateCreate() error {
	verticadblog.Info("validate create", "name", v.Name, "GroupVersion", GroupVersion)

	allErrs := v.validateVerticaDBSpec()
	if allErrs == nil {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaDBKind}, v.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaDB) ValidateUpdate(old runtime.Object) error {
	verticadblog.Info("validate update", "name", v.Name, "GroupVersion", GroupVersion)

	allErrs := append(v.validateImmutableFields(old), v.validateVerticaDBSpec()...)
	if allErrs == nil {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaDBKind}, v.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaDB) ValidateDelete() error {
	verticadblog.Info("validate delete", "name", v.Name, "GroupVersion", GroupVersion)

	return nil
}

func (v *VerticaDB) validateImmutableFields(old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	oldObj := old.(*VerticaDB)
	// kSafety cannot change after creation
	if v.Spec.KSafety != oldObj.Spec.KSafety {
		err := field.Invalid(field.NewPath("spec").Child("kSafety"),
			v.Spec.KSafety,
			"kSafety cannot change after creation.")
		allErrs = append(allErrs, err)
	}

	// initPolicy cannot change after creation
	if v.Spec.InitPolicy != oldObj.Spec.InitPolicy {
		err := field.Invalid(field.NewPath("spec").Child("initPolicy"),
			v.Spec.InitPolicy,
			"initPolicy cannot change after creation.")
		allErrs = append(allErrs, err)
	}
	// dbName cannot change after creation
	if v.Spec.DBName != oldObj.Spec.DBName {
		err := field.Invalid(field.NewPath("spec").Child("dbName"),
			v.Spec.DBName,
			"dbName cannot change after creation.")
		allErrs = append(allErrs, err)
	}
	// communal.path cannot change after creation
	if v.Spec.Communal.Path != oldObj.Spec.Communal.Path {
		err := field.Invalid(field.NewPath("spec").Child("communal").Child("path"),
			v.Spec.Communal.Path,
			"communal.path cannot change after creation")
		allErrs = append(allErrs, err)
	}
	// communal.endpoint cannot change after creation
	if v.Spec.Communal.Endpoint != oldObj.Spec.Communal.Endpoint {
		err := field.Invalid(field.NewPath("spec").Child("communal").Child("endpoint"),
			v.Spec.Communal.Endpoint,
			"communal.endpoint cannot change after creation")
		allErrs = append(allErrs, err)
	}
	// local.storageClass cannot change after creation
	if v.Spec.Local.StorageClass != oldObj.Spec.Local.StorageClass {
		err := field.Invalid(field.NewPath("spec").Child("local").Child("storageClass"),
			v.Spec.Local.StorageClass,
			"local.storageClass cannot change after creation")
		allErrs = append(allErrs, err)
	}
	// when update subcluster names, there should be at least one sc's name match its old name
	if !v.canUpdateScName(oldObj) {
		err := field.Invalid(field.NewPath("spec").Child("subclusters"),
			v.Spec.Subclusters,
			"at least one subcluster name should match its old name")
		allErrs = append(allErrs, err)
	}
	// validate that for existing subclusters that we don't change the
	// primary/secondary type
	if ok, inx := v.isSubclusterTypeIsChanging(oldObj); ok {
		err := field.Invalid(field.NewPath("spec").Child("subclusters").Child("isPrimary"),
			v.Spec.Subclusters[inx],
			fmt.Sprintf("subcluster %s cannot have its isPrimary type change", v.Spec.Subclusters[inx].Name))
		allErrs = append(allErrs, err)
	}
	allErrs = v.checkImmutableUpgradePolicy(oldObj, allErrs)
	allErrs = v.checkImmutableTemporarySubclusterRouting(oldObj, allErrs)
	allErrs = v.checkImmutableEncryptSpreadComm(oldObj, allErrs)
	allErrs = v.checkImmutableLocalPathChange(oldObj, allErrs)
	allErrs = v.checkImmutableShardCount(oldObj, allErrs)
	allErrs = v.checkImmutableS3ServerSideEncryption(oldObj, allErrs)
	allErrs = v.checkImmutableHTTPServerMode(oldObj, allErrs)
	allErrs = v.checkImmutableDepotVolume(oldObj, allErrs)
	return allErrs
}

func (v *VerticaDB) validateVerticaDBSpec() field.ErrorList {
	allErrs := v.hasAtLeastOneSC(field.ErrorList{})
	allErrs = v.hasValidInitPolicy(allErrs)
	allErrs = v.hasValidDBName(allErrs)
	allErrs = v.hasPrimarySubcluster(allErrs)
	allErrs = v.validateKsafety(allErrs)
	allErrs = v.validateCommunalPath(allErrs)
	allErrs = v.validateS3ServerSideEncryption(allErrs)
	allErrs = v.validateAdditionalConfigParms(allErrs)
	allErrs = v.validateCustomLabels(allErrs)
	allErrs = v.validateEndpoint(allErrs)
	allErrs = v.hasValidDomainName(allErrs)
	allErrs = v.hasValidNodePort(allErrs)
	allErrs = v.isNodePortProperlySpecified(allErrs)
	allErrs = v.isServiceTypeValid(allErrs)
	allErrs = v.hasDuplicateScName(allErrs)
	allErrs = v.hasValidVolumeName(allErrs)
	allErrs = v.hasValidVolumeMountName(allErrs)
	allErrs = v.hasValidKerberosSetup(allErrs)
	allErrs = v.hasValidTemporarySubclusterRouting(allErrs)
	allErrs = v.matchingServiceNamesAreConsistent(allErrs)
	allErrs = v.transientSubclusterMustMatchTemplate(allErrs)
	allErrs = v.validateRequeueTimes(allErrs)
	allErrs = v.validateEncryptSpreadComm(allErrs)
	allErrs = v.validateLocalStorage(allErrs)
	allErrs = v.validateHTTPServerMode(allErrs)
	allErrs = v.hasValidShardCount(allErrs)
	allErrs = v.hasValidProbeOverrides(allErrs)
	if len(allErrs) == 0 {
		return nil
	}
	return allErrs
}

func (v *VerticaDB) hasAtLeastOneSC(allErrs field.ErrorList) field.ErrorList {
	// there should be at least one subcluster defined
	if len(v.Spec.Subclusters) == 0 {
		err := field.Invalid(field.NewPath("spec").Child("subclusters"),
			v.Spec.Subclusters,
			`there should be at least one subcluster defined`)
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) hasValidInitPolicy(allErrs field.ErrorList) field.ErrorList {
	switch v.Spec.InitPolicy {
	case CommunalInitPolicyCreate:
	case CommunalInitPolicyCreateSkipPackageInstall:
	case CommunalInitPolicyRevive:
	case CommunalInitPolicyScheduleOnly:
	default:
		err := field.Invalid(field.NewPath("spec").Child("initPolicy"),
			v.Spec.InitPolicy,
			fmt.Sprintf("initPolicy should either be %s, %s, %s or %s",
				CommunalInitPolicyCreate, CommunalInitPolicyCreateSkipPackageInstall,
				CommunalInitPolicyRevive, CommunalInitPolicyScheduleOnly))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) validateCommunalPath(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.InitPolicy == CommunalInitPolicyScheduleOnly {
		return allErrs
	}
	if v.Spec.Communal.Path != "" {
		return allErrs
	}
	err := field.Invalid(field.NewPath("spec").Child("communal").Child("path"),
		v.Spec.Communal.Path,
		"communal.path cannot be empty")
	return append(allErrs, err)
}

func (v *VerticaDB) validateS3ServerSideEncryption(allErrs field.ErrorList) field.ErrorList {
	if !v.IsS3() || v.Spec.Communal.S3ServerSideEncryption == "" {
		return allErrs
	}
	if !v.IsKnownSseType() {
		err := field.Invalid(field.NewPath("spec").Child("communal").Child("s3ServerSideEncryption"),
			v.Spec.Communal.S3ServerSideEncryption,
			fmt.Sprintf("communal.s3ServerSideEncryption, if specified, can only be %s, %s or %s",
				SseS3, SseKMS, SseC))
		allErrs = append(allErrs, err)
	}
	if v.IsSseKMS() {
		value, found := v.Spec.Communal.AdditionalConfig[S3SseKmsKeyID]
		if !found || value == "" {
			err := field.Invalid(field.NewPath("spec").Child("communal").Child("additionalConfig"),
				v.Spec.Communal.AdditionalConfig,
				fmt.Sprintf("communal.additionalconfig[%s] must be set when setting up SSE-KMS server-side encryption",
					S3SseKmsKeyID))
			allErrs = append(allErrs, err)
		}
	}
	if v.IsSseC() {
		if v.Spec.Communal.S3SseCustomerKeySecret == "" {
			err := field.Invalid(field.NewPath("spec").Child("communal").Child("s3SseCustomerKeySecret"),
				v.Spec.Communal.S3SseCustomerKeySecret,
				"communal.3SseCustomerKeySecret must be set when setting up SSE-C server-side encryption")
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) validateAdditionalConfigParms(allErrs field.ErrorList) field.ErrorList {
	if v.IsAdditionalConfigMapEmpty() {
		return allErrs
	}
	additionalConfigKeysCopy := map[string]string{}
	// additional config parms are case insensitive so we need to check that
	// if, for example, awsauth and AWSauth are passed, they are seen as duplicates.
	for k := range v.Spec.Communal.AdditionalConfig {
		_, ok := additionalConfigKeysCopy[strings.ToLower(k)]
		if ok {
			err := field.Invalid(field.NewPath("spec").Child("communal").Child("additionalConfig"),
				v.Spec.Communal.AdditionalConfig,
				fmt.Sprintf("duplicates key %s", k))
			allErrs = append(allErrs, err)
		}
		additionalConfigKeysCopy[strings.ToLower(k)] = ""
	}
	return allErrs
}

func (v *VerticaDB) validateCustomLabels(allErrs field.ErrorList) field.ErrorList {
	for _, invalidLabel := range vmeta.ProtectedLabels {
		_, ok := v.Spec.Labels[invalidLabel]
		if ok {
			err := field.Invalid(field.NewPath("spec").Child("labels"),
				v.Spec.Labels,
				fmt.Sprintf("'%s' is a restricted label.", invalidLabel))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) validateEndpoint(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.InitPolicy == CommunalInitPolicyScheduleOnly {
		return allErrs
	}
	// This only applies if it's S3 or GCP.
	if !v.IsS3() && !v.IsGCloud() {
		return allErrs
	}
	// communal.endpoint must be prefaced with http:// or https:// to know what protocol to connect with.
	if !(strings.HasPrefix(v.Spec.Communal.Endpoint, "http://") ||
		strings.HasPrefix(v.Spec.Communal.Endpoint, "https://")) {
		err := field.Invalid(field.NewPath("spec").Child("communal").Child("endpoint"),
			v.Spec.Communal.Endpoint,
			"communal.endpoint must be prefaced with http:// or https:// to know what protocol to connect with")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) hasValidDBName(allErrs field.ErrorList) field.ErrorList {
	dbName := v.Spec.DBName
	if len(dbName) > dbNameLengthLimit {
		err := field.Invalid(field.NewPath("spec").Child("dbName"),
			v.Spec.DBName,
			"dbName cannot exceed 30 characters")
		allErrs = append(allErrs, err)
	}
	invalidChar := invalidDBNameChars
	for _, c := range invalidChar {
		if strings.Contains(dbName, string(c)) {
			err := field.Invalid(field.NewPath("spec").Child("dbName"),
				v.Spec.DBName,
				fmt.Sprintf(`dbName cannot have the '%s' character`, string(c)))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) hasPrimarySubcluster(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.IsPrimary {
			return allErrs
		}
	}

	err := field.Invalid(field.NewPath("spec").Child("subclusters"),
		v.Spec.Subclusters,
		`there must be at least one primary subcluster`)
	return append(allErrs, err)
}

func (v *VerticaDB) validateKsafety(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.InitPolicy == CommunalInitPolicyScheduleOnly {
		return allErrs
	}
	sizeSum := v.getClusterSize()
	switch v.Spec.KSafety {
	case KSafety0:
		if sizeSum < KSafety0MinHosts || sizeSum > KSafety0MaxHosts {
			err := field.Invalid(field.NewPath("spec").Child("kSafety"),
				v.Spec.KSafety,
				fmt.Sprintf("with kSafety 0, the total size of the cluster must have between %d and %d hosts", KSafety0MinHosts, KSafety0MaxHosts))
			allErrs = append(allErrs, err)
		}
	case KSafety1:
		if sizeSum < KSafety1MinHosts {
			err := field.Invalid(field.NewPath("spec").Child("kSafety"),
				v.Spec.KSafety,
				fmt.Sprintf("with kSafety 1, the total size of the cluster must have at least %d hosts", KSafety1MinHosts))
			allErrs = append(allErrs, err)
		}
	default:
		err := field.Invalid(field.NewPath("spec").Child("kSafety"),
			v.Spec.KSafety,
			fmt.Sprintf("kSafety can only be %s or %s", KSafety0, KSafety1))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) getClusterSize() int {
	sizeSum := 0
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		sizeSum += int(sc.Size)
	}
	return sizeSum
}

func (v *VerticaDB) hasValidDomainName(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if !IsValidSubclusterName(sc.GenCompatibleFQDN()) {
			err := field.Invalid(field.NewPath("spec").Child("subcluster").Index(i).Child("name"),
				v.Spec.Subclusters[i],
				"is not a valid domain name")
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func isNodePortNumberInvalid(port int32) bool {
	return port != 0 && (port < portLowerBound || port > portUpperBound)
}

func (v *VerticaDB) genNodePortInvalidError(allErrs field.ErrorList, pathPrefix *field.Path, nodePortName string, nodePortVal int32) field.ErrorList {
	err := field.Invalid(pathPrefix.Child(nodePortName),
		nodePortVal,
		fmt.Sprintf(`%s must be 0 or in the range of %d-%d`, nodePortName, portLowerBound, portUpperBound))
	return append(allErrs, err)
}

func (v *VerticaDB) hasValidNodePort(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.ServiceType == v1.ServiceTypeNodePort {
			pathPrefix := field.NewPath("spec").Child("subclusters").Index(i)
			if isNodePortNumberInvalid(sc.NodePort) {
				allErrs = v.genNodePortInvalidError(allErrs, pathPrefix,
					"nodePort", v.Spec.Subclusters[i].NodePort)
			}
			if isNodePortNumberInvalid(sc.VerticaHTTPNodePort) {
				allErrs = v.genNodePortInvalidError(allErrs, pathPrefix,
					"verticaHttpNodePort", v.Spec.Subclusters[i].VerticaHTTPNodePort)
			}
		}
	}
	return allErrs
}

func (v *VerticaDB) isNodePortProperlySpecified(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		// NodePort can only be set for LoadBalancer and NodePort
		if sc.ServiceType != v1.ServiceTypeLoadBalancer && sc.ServiceType != v1.ServiceTypeNodePort {
			if sc.NodePort != 0 {
				err := field.Invalid(field.NewPath("spec").Child("subclusters").Index(i).Child("nodePort"),
					v.Spec.Subclusters[i].NodePort,
					fmt.Sprintf("nodePort can only be specified for service types %s and %s",
						v1.ServiceTypeLoadBalancer, v1.ServiceTypeNodePort))
				allErrs = append(allErrs, err)
			}
		}
	}
	return allErrs
}

func (v *VerticaDB) isServiceTypeValid(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		switch sc.ServiceType {
		case v1.ServiceTypeLoadBalancer, v1.ServiceTypeNodePort, v1.ServiceTypeExternalName, v1.ServiceTypeClusterIP:
			// Valid types
		default:
			err := field.Invalid(field.NewPath("spec").Child("subclusters").Index(i).Child("serviceType"),
				v.Spec.Subclusters[i].ServiceType,
				"not a valid service type")
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) hasDuplicateScName(allErrs field.ErrorList) field.ErrorList {
	countSc := len(v.Spec.Subclusters)
	for i := 0; i < countSc-1; i++ {
		sc1 := &v.Spec.Subclusters[i]
		for j := i + 1; j < countSc; j++ {
			sc2 := &v.Spec.Subclusters[j]
			// subcluster names like default-subcluster and default_subcluster
			// are considered identical
			if sc1.GenCompatibleFQDN() == sc2.GenCompatibleFQDN() {
				err := field.Invalid(field.NewPath("spec").Child("subclusters").Index(j).Child("name"),
					v.Spec.Subclusters[j].Name,
					fmt.Sprintf("duplicates the name of subcluster[%d]", i))
				allErrs = append(allErrs, err)
			}
		}
	}
	return allErrs
}

func (v *VerticaDB) hasValidVolumeName(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Volumes {
		vol := v.Spec.Volumes[i]
		if (vol.Name == LocalDataPVC) || (vol.Name == PodInfoMountName) || (vol.Name == LicensingMountName) || (vol.Name == HadoopConfigMountName) {
			err := field.Invalid(field.NewPath("spec").Child("volumes").Index(i).Child("name"),
				v.Spec.Volumes[i].Name,
				"conflicts with the name of one of the internally generated volumes")
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// hasValidVolumeMountName checks wether any of the custom volume mounts added
// shared a name with any of the generated paths.
func (v *VerticaDB) hasValidVolumeMountName(allErrs field.ErrorList) field.ErrorList {
	invalidPaths := make([]string, len(paths.MountPaths))
	copy(invalidPaths, paths.MountPaths)
	invalidPaths = append(invalidPaths, v.Spec.Local.DataPath, v.Spec.Local.DepotPath, v.Spec.Local.GetCatalogPath())
	for i := range v.Spec.VolumeMounts {
		volMnt := v.Spec.VolumeMounts[i]
		for j := range invalidPaths {
			if volMnt.MountPath == invalidPaths[j] {
				err := field.Invalid(field.NewPath("spec").Child("volumeMounts").Index(i).Child("mountPath"),
					volMnt,
					"conflicts with the mount path of one of the internally generated paths")
				allErrs = append(allErrs, err)
			}
		}
		if strings.HasPrefix(volMnt.MountPath, paths.CertsRoot) {
			err := field.Invalid(field.NewPath("spec").Child("volumeMounts").Index(i).Child("mountPath"),
				v.Spec.VolumeMounts[i].MountPath,
				fmt.Sprintf("cannot shared the same path prefix as the certs root '%s'", paths.CertsRoot))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) canUpdateScName(oldObj *VerticaDB) bool {
	scMap := map[string]*Subcluster{}
	for i := range oldObj.Spec.Subclusters {
		sc := &oldObj.Spec.Subclusters[i]
		if len(oldObj.Status.Subclusters) > i {
			scStatus := &oldObj.Status.Subclusters[i]
			if scStatus.AddedToDBCount > 0 {
				scMap[sc.Name] = sc
			}
		}
	}
	canUpdate := false
	if len(scMap) > 0 {
		for i := range v.Spec.Subclusters {
			scNew := &v.Spec.Subclusters[i]
			if _, ok := scMap[scNew.Name]; ok {
				canUpdate = true
			}
		}
	} else {
		canUpdate = true
	}
	return canUpdate
}

// hasValidKerberosSetup checks whether Kerberos settings are correct
func (v *VerticaDB) hasValidKerberosSetup(allErrs field.ErrorList) field.ErrorList {
	// Handle two valid cases.  None of the Kerberos settings are used or they
	// all are.  This will detect cases when only a portion of them are set.
	if (v.Spec.Communal.KerberosRealm == "" && v.Spec.Communal.KerberosServiceName == "") ||
		(v.Spec.Communal.KerberosRealm != "" && v.Spec.Communal.KerberosServiceName != "" && v.Spec.KerberosSecret != "") {
		return allErrs
	}

	if v.Spec.Communal.KerberosRealm == "" {
		err := field.Invalid(field.NewPath("spec").Child("communal").Child("kerberosRealm"),
			v.Spec.Communal.KerberosRealm,
			"kerberosRealm must be set if setting up Kerberos")
		allErrs = append(allErrs, err)
	}
	if v.Spec.Communal.KerberosServiceName == "" {
		err := field.Invalid(field.NewPath("spec").Child("communal").Child("kerberosServiceName"),
			v.Spec.Communal.KerberosServiceName,
			"kerberosServiceName must be set if setting up Kerberos")
		allErrs = append(allErrs, err)
	}
	if v.Spec.KerberosSecret == "" {
		err := field.Invalid(field.NewPath("spec").Child("kerberosSecret"),
			v.Spec.KerberosSecret,
			"kerberosSecret must be set if setting up Kerberos")
		allErrs = append(allErrs, err)
	}

	return allErrs
}

// hasValidTemporarySubclusterRouting verifies the contents of
// temporarySubclusterRouting are valid
func (v *VerticaDB) hasValidTemporarySubclusterRouting(allErrs field.ErrorList) field.ErrorList {
	scMap := v.GenSubclusterMap()
	fieldPrefix := field.NewPath("spec").Child("temporarySubclusterRouting")
	if v.Spec.TemporarySubclusterRouting.Template.Name != "" {
		templateFieldPrefix := fieldPrefix.Child("template")
		if v.Spec.TemporarySubclusterRouting.Template.IsPrimary {
			err := field.Invalid(templateFieldPrefix.Child("isPrimary"),
				v.Spec.TemporarySubclusterRouting.Template.IsPrimary,
				"subcluster template must be a secondary subcluster")
			allErrs = append(allErrs, err)
		}
		if v.Spec.TemporarySubclusterRouting.Template.Size == 0 {
			err := field.Invalid(templateFieldPrefix.Child("size"),
				v.Spec.TemporarySubclusterRouting.Template.Size,
				"size of subcluster template must be greater than zero")
			allErrs = append(allErrs, err)
		}
		if sc, ok := scMap[v.Spec.TemporarySubclusterRouting.Template.Name]; ok && !sc.IsTransient {
			err := field.Invalid(templateFieldPrefix.Child("name"),
				v.Spec.TemporarySubclusterRouting.Template.Name,
				"cannot choose a name of an existing subcluster")
			allErrs = append(allErrs, err)
		}
	}
	for i := range v.Spec.TemporarySubclusterRouting.Names {
		if _, ok := scMap[v.Spec.TemporarySubclusterRouting.Names[i]]; !ok {
			err := field.Invalid(fieldPrefix.Child("names").Index(i),
				v.Spec.TemporarySubclusterRouting.Names[i],
				"name must be an existing subcluster")
			allErrs = append(allErrs, err)
		}
	}
	if len(v.Spec.TemporarySubclusterRouting.Names) > 0 && v.RequiresTransientSubcluster() {
		err := field.Invalid(fieldPrefix,
			v.Spec.TemporarySubclusterRouting,
			"cannot use a template and a list of subcluster names at the same time")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) isSubclusterTypeIsChanging(oldObj *VerticaDB) (ok bool, scInx int) {
	// Create a map of subclusterName -> isPrimary using the old object.
	nameToPrimaryMap := map[string]bool{}
	for i := range oldObj.Spec.Subclusters {
		sc := oldObj.Spec.Subclusters[i]
		nameToPrimaryMap[sc.Name] = sc.IsPrimary
	}
	// Go through new object to see that IsPrimary isn't changing for any
	// existing subcluster
	for i := range v.Spec.Subclusters {
		sc := v.Spec.Subclusters[i]
		isPrimary, ok := nameToPrimaryMap[sc.Name]
		if ok && isPrimary != sc.IsPrimary {
			return true, i
		}
	}
	return false, 0
}

// matchingServiceNamesAreConsistent ensures that any subclusters that share the
// same service name have matching values in them that pertain to the service object.
func (v *VerticaDB) matchingServiceNamesAreConsistent(allErrs field.ErrorList) field.ErrorList {
	processedServiceName := map[string]bool{}

	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if _, ok := processedServiceName[sc.GetServiceName()]; ok {
			continue
		}
		for j := i + 1; j < len(v.Spec.Subclusters); j++ {
			osc := &v.Spec.Subclusters[j]
			if sc.GetServiceName() == osc.GetServiceName() {
				fieldPrefix := field.NewPath("spec").Child("subclusters").Index(j)
				if !reflect.DeepEqual(sc.ExternalIPs, osc.ExternalIPs) {
					err := field.Invalid(fieldPrefix.Child("externalIPs").Index(i),
						sc.ExternalIPs,
						"externalIPs don't match other subcluster(s) sharing the same serviceName")
					allErrs = append(allErrs, err)
				}
				if sc.LoadBalancerIP != osc.LoadBalancerIP {
					err := field.Invalid(fieldPrefix.Child("loadBalancerIP").Index(i),
						sc.LoadBalancerIP,
						"loadBalancerIP doesn't match other subcluster(s) sharing the same serviceName")
					allErrs = append(allErrs, err)
				}
				if !reflect.DeepEqual(sc.ServiceAnnotations, osc.ServiceAnnotations) {
					err := field.Invalid(fieldPrefix.Child("serviceAnnotations").Index(i),
						sc.ServiceAnnotations,
						"serviceAnnotations don't match other subcluster(s) sharing the same serviceName")
					allErrs = append(allErrs, err)
				}
				if sc.NodePort != osc.NodePort {
					err := field.Invalid(fieldPrefix.Child("nodePort").Index(i),
						sc.NodePort,
						"nodePort doesn't match other subcluster(s) sharing the same serviceName")
					allErrs = append(allErrs, err)
				}
				if sc.ServiceType != osc.ServiceType {
					err := field.Invalid(fieldPrefix.Child("serviceType").Index(i),
						sc.ServiceType,
						"serviceType doesn't match other subcluster(s) sharing the same serviceName")
					allErrs = append(allErrs, err)
				}
			}
		}
		// Set a flag so that we don't process this service name in another subcluster
		processedServiceName[sc.GetServiceName()] = true
	}
	return allErrs
}

// transientSubclusterMustMatchTemplate is a check to make sure the IsTransient
// isn't being set for subcluster.  It must only be used for the temporary
// subcluster template.
func (v *VerticaDB) transientSubclusterMustMatchTemplate(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if !sc.IsTransient {
			continue
		}

		fieldPrefix := field.NewPath("spec").Child("subclusters").Index(i)
		if sc.Name != v.Spec.TemporarySubclusterRouting.Template.Name {
			err := field.Invalid(fieldPrefix.Child("Name").Index(i),
				sc.Name,
				"Transient subcluster name doesn't match template")
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// validateRequeueTimes is a check for the various requeue times in the CR.
func (v *VerticaDB) validateRequeueTimes(allErrs field.ErrorList) field.ErrorList {
	prefix := field.NewPath("spec")
	if v.Spec.RequeueTime < 0 {
		err := field.Invalid(prefix.Child("requeueTime"),
			v.Spec.RequeueTime,
			"requeueTime cannot be negative")
		allErrs = append(allErrs, err)
	}
	if v.Spec.UpgradeRequeueTime < 0 {
		err := field.Invalid(prefix.Child("upgradeRequeueTime"),
			v.Spec.UpgradeRequeueTime,
			"upgradeRequeueTime cannot be negative")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) validateEncryptSpreadComm(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.EncryptSpreadComm != "" && v.Spec.EncryptSpreadComm != EncryptSpreadCommWithVertica {
		err := field.Invalid(field.NewPath("spec").Child("encrpytSpreadComm"),
			v.Spec.EncryptSpreadComm,
			fmt.Sprintf("encryptSpreadComm can either be an empty string or set to %s", EncryptSpreadCommWithVertica))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) validateLocalStorage(allErrs field.ErrorList) field.ErrorList {
	allErrs = v.validateLocalPaths(allErrs)
	return v.validateDepotVolume(allErrs)
}

func (v *VerticaDB) validateLocalPaths(allErrs field.ErrorList) field.ErrorList {
	// We cannot let any of the local paths be the same as important paths in
	// the image.  Otherwise, we risk losing the contents of those directory in
	// the container, which can mess up the deployment.
	invalidPaths := []string{
		"/home",
		"/home/dbadmin",
		"/opt",
		"/opt/vertica",
		"/opt/vertica/bin",
		"/opt/vertica/sbin",
		"/opt/vertica/include",
		"/opt/vertica/java",
		"/opt/vertica/lib",
		"/opt/vertica/oss",
		"/opt/vertica/packages",
		"/opt/vertica/share",
		"/opt/vertica/scripts",
		"/opt/vertica/spread",
	}
	for _, invalidPath := range invalidPaths {
		if v.Spec.Local.DataPath != invalidPath && v.Spec.Local.DepotPath != invalidPath &&
			v.Spec.Local.CatalogPath != invalidPath {
			continue
		}
		var fieldRef interface{}
		var fieldPathName string
		if v.Spec.Local.DataPath == invalidPath {
			fieldPathName = "dataPath"
			fieldRef = v.Spec.Local.DataPath
		} else if v.Spec.Local.CatalogPath == invalidPath {
			fieldPathName = "catalogPath"
			fieldRef = v.Spec.Local.CatalogPath
		} else {
			fieldPathName = "depotPath"
			fieldRef = v.Spec.Local.DepotPath
		}
		err := field.Invalid(field.NewPath("spec").Child("local").Child(fieldPathName),
			fieldRef, fmt.Sprintf("%s cannot be set to %s. This is a restricted path.", fieldPathName, invalidPath))
		allErrs = append(allErrs, err)
	}
	// When depotVolume is EmptyDir, depotPath must be different from data and catalog paths.
	if v.IsDepotVolumeEmptyDir() {
		if !v.Spec.Local.IsDepotPathUnique() {
			err := field.Invalid(field.NewPath("spec").Child("local").Child("depotPath"),
				v.Spec.Local.DepotPath, "depotPath cannot be equal to dataPath or catalogPath when depotVolume is EmptyDir")
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) validateDepotVolume(allErrs field.ErrorList) field.ErrorList {
	if !v.IsKnownDepotVolumeType() {
		err := field.Invalid(field.NewPath("spec").Child("local").Child("depotVolume"),
			v.Spec.Local.DepotVolume,
			fmt.Sprintf("valid values are %s, %s or an empty string", EmptyDir, PersistentVolume))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) validateHTTPServerMode(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.DeprecatedHTTPServerMode == "" ||
		v.Spec.DeprecatedHTTPServerMode == HTTPServerModeEnabled ||
		v.Spec.DeprecatedHTTPServerMode == HTTPServerModeDisabled ||
		v.Spec.DeprecatedHTTPServerMode == HTTPServerModeAuto {
		return allErrs
	}

	err := field.Invalid(field.NewPath("spec").Child("httpServerMode"),
		v.Spec.DeprecatedHTTPServerMode,
		fmt.Sprintf("Valid values are: %s, %s, %s or an empty string",
			HTTPServerModeAuto, HTTPServerModeEnabled, HTTPServerModeDisabled))
	return append(allErrs, err)
}

func (v *VerticaDB) hasValidShardCount(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.ShardCount > 0 {
		return allErrs
	}

	err := field.Invalid(field.NewPath("spec").Child("shardCount"),
		v.Spec.ShardCount,
		"Shard count must be > 0")
	return append(allErrs, err)
}

func (v *VerticaDB) hasValidProbeOverrides(allErrs field.ErrorList) field.ErrorList {
	parentField := field.NewPath("spec")
	allErrs = v.hasValidProbeOverride(allErrs, parentField.Child("readinessProbeOverride"), v.Spec.ReadinessProbeOverride)
	allErrs = v.hasValidProbeOverride(allErrs, parentField.Child("startupProbeOverride"), v.Spec.StartupProbeOverride)
	allErrs = v.hasValidProbeOverride(allErrs, parentField.Child("livenessProbeOverride"), v.Spec.LivenessProbeOverride)
	return allErrs
}

func (v *VerticaDB) hasValidProbeOverride(allErrs field.ErrorList, fieldPath *field.Path, probe *v1.Probe) field.ErrorList {
	if probe == nil {
		return allErrs
	}
	// There are different kinds of handlers. You are only allowed to specify no more than one.
	handlerCount := 0
	if probe.Exec != nil {
		handlerCount++
	}
	if probe.TCPSocket != nil {
		handlerCount++
	}
	if probe.GRPC != nil {
		handlerCount++
	}
	if probe.HTTPGet != nil {
		handlerCount++
	}
	if handlerCount > 1 {
		err := field.Invalid(fieldPath, probe, "can only specify one handler in the override")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) isImageChangeInProgress() bool {
	return v.isConditionIndexSet(ImageChangeInProgressIndex)
}

func (v *VerticaDB) isDBInitialized() bool {
	return v.isConditionIndexSet(DBInitializedIndex)
}

// checkImmutableUpgradePolicy will see if it unsafe to change the
// upgradePolicy.  It will log an error if it detects a change in that field
// when it isn't allowed.
func (v *VerticaDB) checkImmutableUpgradePolicy(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	if v.Spec.UpgradePolicy == oldObj.Spec.UpgradePolicy ||
		!oldObj.isImageChangeInProgress() {
		return allErrs
	}
	err := field.Invalid(field.NewPath("spec").Child("upgradePolicy"),
		v.Spec.UpgradePolicy,
		"upgradePolicy cannot change because upgrade is in progress")
	allErrs = append(allErrs, err)
	return allErrs
}

// checkImmutableTemporarySubclusterRouting will check if
// temporarySubclusterRouting is changing when it isn't allowed to.
func (v *VerticaDB) checkImmutableTemporarySubclusterRouting(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// TemporarySubclusterRouting is allowed to change as long as an image
	// change isn't in progress
	if !oldObj.isImageChangeInProgress() {
		return allErrs
	}
	if !reflect.DeepEqual(v.Spec.TemporarySubclusterRouting.Names, oldObj.Spec.TemporarySubclusterRouting.Names) {
		err := field.Invalid(field.NewPath("spec").Child("temporarySubclusterRouting").Child("names"),
			v.Spec.TemporarySubclusterRouting.Names,
			"subcluster names for temporasySubclusterRouting cannot change when an upgrade is in progress")
		allErrs = append(allErrs, err)
	}
	if !reflect.DeepEqual(v.Spec.TemporarySubclusterRouting.Template, oldObj.Spec.TemporarySubclusterRouting.Template) {
		err := field.Invalid(field.NewPath("spec").Child("temporarySubclusterRouting").Child("template"),
			v.Spec.TemporarySubclusterRouting.Template,
			"template for temporasySubclusterRouting cannot change when an upgrade is in progress")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) checkImmutableEncryptSpreadComm(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	if v.Spec.EncryptSpreadComm != oldObj.Spec.EncryptSpreadComm {
		err := field.Invalid(field.NewPath("spec").Child("encryptSpreadComm"),
			v.Spec.EncryptSpreadComm,
			"encryptSpreadComm cannot change after creation")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// checkImmutableLocalPathChange will make sure none of the local paths change
// after the database has been initialized.
func (v *VerticaDB) checkImmutableLocalPathChange(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// We allow the paths to change as long as the DB isn't yet initialized.
	if !v.isDBInitialized() {
		return allErrs
	}

	pathPrefix := field.NewPath("spec").Child("local")
	if v.Spec.Local.DataPath != oldObj.Spec.Local.DataPath {
		err := field.Invalid(pathPrefix.Child("dataPath"),
			v.Spec.Local.DataPath,
			"dataPath cannot change after the DB has been initialized.")
		allErrs = append(allErrs, err)
	}
	if v.Spec.Local.DepotPath != oldObj.Spec.Local.DepotPath {
		err := field.Invalid(pathPrefix.Child("depotPath"),
			v.Spec.Local.DepotPath,
			"depotPath cannot change after the DB has been initialized.")
		allErrs = append(allErrs, err)
	}
	if v.Spec.Local.GetCatalogPath() != oldObj.Spec.Local.GetCatalogPath() {
		err := field.Invalid(pathPrefix.Child("catalogPath"),
			v.Spec.Local.CatalogPath,
			"catalogPath cannot change after the DB has been initialized.")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// checkImmutableShardCount will make sure the shard count doesn't change after
// the db has been initialized.
func (v *VerticaDB) checkImmutableShardCount(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	if !v.isDBInitialized() {
		return allErrs
	}
	if v.Spec.ShardCount != oldObj.Spec.ShardCount {
		err := field.Invalid(field.NewPath("spec").Child("shardCount"),
			v.Spec.ShardCount,
			"shardCount cannot change after creation.")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// checkImmutableS3ServerSideEncryption will make sure communal.s3ServerSideEncryption
// does not change after creation
func (v *VerticaDB) checkImmutableS3ServerSideEncryption(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	if v.Spec.Communal.S3ServerSideEncryption != oldObj.Spec.Communal.S3ServerSideEncryption {
		err := field.Invalid(field.NewPath("spec").Child("communal").Child("s3ServerSideEncryption"),
			v.Spec.Communal.S3ServerSideEncryption,
			"communal.s3ServerSideEncryption cannot change after creation")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// checkImmutableHTTPServerMode will make sure httpServerMode does not changed in any
// inappropriate way like Enabled -> Disabled, Auto -> Disabled, Enabled -> Auto.
func (v *VerticaDB) checkImmutableHTTPServerMode(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	isTransitionAutoToDisabled := oldObj.IsHTTPServerAuto() && v.IsHTTPServerDisabled()
	if v.Spec.DeprecatedHTTPServerMode != oldObj.Spec.DeprecatedHTTPServerMode {
		if oldObj.Spec.DeprecatedHTTPServerMode == HTTPServerModeEnabled ||
			isTransitionAutoToDisabled {
			err := field.Invalid(field.NewPath("spec").Child("httpServerMode"),
				v.Spec.DeprecatedHTTPServerMode,
				fmt.Sprintf("transition from '%s' to '%s' not allowed",
					oldObj.Spec.DeprecatedHTTPServerMode, v.Spec.DeprecatedHTTPServerMode))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// checkImmutableDepotVolume will make sure local.depotVolume
// does not change after the db has been initialized.
func (v *VerticaDB) checkImmutableDepotVolume(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	if !v.isDBInitialized() {
		return allErrs
	}
	if v.Spec.Local.DepotVolume != oldObj.Spec.Local.DepotVolume {
		err := field.Invalid(field.NewPath("spec").Child("local").Child("depotVolume"),
			v.Spec.Local.DepotVolume,
			"cannot change after the db has been initialized")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// setDefaultServiceName will explicitly set the serviceName in any subcluster
// that omitted it
func (v *VerticaDB) setDefaultServiceName() {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.ServiceName == "" {
			sc.ServiceName = sc.GetServiceName()
		}
	}
}
