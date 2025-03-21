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

//nolint:lll
package v1

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	vutil "github.com/vertica/vcluster/vclusterops/util"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	vversion "github.com/vertica/vertica-kubernetes/pkg/version"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	invalidDBNameChars    = "$=<>`" + `'^\".@*?#&/-:;{}()[] \~!%+|,`
	dbNameLengthLimit     = 30
	KSafety0MinHosts      = 1
	KSafety0MaxHosts      = 3
	KSafety1MinHosts      = 3
	portLowerBound        = 30000
	portUpperBound        = 32767
	LocalDataPVC          = "local-data"
	PodInfoMountName      = "podinfo"
	LicensingMountName    = "licensing"
	HadoopConfigMountName = "hadoop-conf"
	Krb5SecretMountName   = "krb5"
	SSHMountName          = "ssh"
	NMACertsMountName     = "nma-certs"
	NMATLSConfigMapName   = "nma-tls-config"
	DepotMountName        = "depot"
	S3Prefix              = "s3://"
	GCloudPrefix          = "gs://"
	AzurePrefix           = "azb://"
	trueString            = "true"
	VProxyDefaultImage    = "opentext/client-proxy:latest"
	VProxyDefaultReplicas = 1
)

// hdfsPrefixes are prefixes for an HDFS path.
var hdfsPrefixes = []string{"webhdfs://", "swebhdfs://"}

// validProxyLogLevel are acceptable values for proxy log level annotation
var validProxyLogLevel = []string{"TRACE", "DEBUG", "INFO", "WARN", "FATAL", "NONE"}

// log is for logging in this package.
var verticadblog = logf.Log.WithName("verticadb-resource")

func (v *VerticaDB) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(v).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-vertica-com-v1-verticadb,mutating=true,failurePolicy=fail,sideEffects=None,groups=vertica.com,resources=verticadbs,verbs=create;update,versions=v1,name=mverticadb.kb.io,admissionReviewVersions=v1

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
	if v.Spec.TemporarySubclusterRouting != nil {
		v.Spec.TemporarySubclusterRouting.Template.Type = SecondarySubcluster
	}
	v.setDefaultServiceName()
	v.setDefaultSandboxImages()
	v.setDefaultProxy()
}

var _ webhook.Validator = &VerticaDB{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaDB) ValidateCreate() (admission.Warnings, error) {
	verticadblog.Info("validate create", "name", v.Name, "GroupVersion", GroupVersion)

	allErrs := v.validateVerticaDBSpec()
	if len(allErrs) == 0 {
		return nil, nil
	}
	return nil, apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaDBKind}, v.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaDB) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	verticadblog.Info("validate update", "name", v.Name, "GroupVersion", GroupVersion)

	allErrs := append(v.validateImmutableFields(old), v.validateVerticaDBSpec()...)

	if len(allErrs) == 0 {
		return nil, nil
	}
	return nil, apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaDBKind}, v.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaDB) ValidateDelete() (admission.Warnings, error) {
	verticadblog.Info("validate delete", "name", v.Name, "GroupVersion", GroupVersion)

	return nil, nil
}

func (v *VerticaDB) validateImmutableFields(old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	oldObj := old.(*VerticaDB)

	allErrs = v.checkImmutableBasic(oldObj, allErrs)
	allErrs = v.checkImmutableUpgradePolicy(oldObj, allErrs)
	allErrs = v.checkImmutableDeploymentMethod(oldObj, allErrs)
	allErrs = v.checkImmutableTemporarySubclusterRouting(oldObj, allErrs)
	allErrs = v.checkImmutableEncryptSpreadComm(oldObj, allErrs)
	allErrs = v.checkImmutableLocalPathChange(oldObj, allErrs)
	allErrs = v.checkImmutableShardCount(oldObj, allErrs)
	allErrs = v.checkImmutableS3ServerSideEncryption(oldObj, allErrs)
	allErrs = v.checkImmutableDepotVolume(oldObj, allErrs)
	allErrs = v.checkImmutablePodSecurityContext(oldObj, allErrs)
	allErrs = v.checkImmutableSubclusterDuringUpgrade(oldObj, allErrs)
	allErrs = v.checkImmutableSubclusterInSandbox(oldObj, allErrs)
	allErrs = v.checkImmutableStsName(oldObj, allErrs)
	allErrs = v.checkImmutableClientProxy(oldObj, allErrs)
	allErrs = v.checkValidSubclusterTypeTransition(oldObj, allErrs)
	allErrs = v.checkSandboxesDuringUpgrade(oldObj, allErrs)
	allErrs = v.checkShutdownSandboxImage(oldObj, allErrs)
	allErrs = v.checkShutdownForSandboxesToBeRemoved(oldObj, allErrs)
	allErrs = v.checkShutdownForSubclustersToBeRemoved(oldObj, allErrs)
	allErrs = v.checkUnsandboxShutdownConditions(oldObj, allErrs)
	allErrs = v.checkSubclustersInShutdownSandbox(oldObj, allErrs)
	allErrs = v.checkNewSBoxOrSClusterShutdownUnset(allErrs)
	allErrs = v.checkSClusterToBeSandboxedShutdownUnset(allErrs)
	allErrs = v.checkShutdownForScaleOutOrIn(oldObj, allErrs)
	return allErrs
}

func (v *VerticaDB) checkValidSubclusterTypeTransition(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// Create a map of subclusterName -> type using the old object.
	nameToTypeMap := map[string]string{}
	for i := range oldObj.Spec.Subclusters {
		sc := oldObj.Spec.Subclusters[i]
		nameToTypeMap[sc.Name] = sc.Type
	}
	scToSbMap := v.GenSubclusterSandboxMap()
	// Helper function to log an error
	invalidStateTransitionErr := func(inx int) {
		path := field.NewPath("spec").Child("subclusters").Index(inx).Child("type")
		err := field.Invalid(path,
			v.Spec.Subclusters[inx].Type,
			fmt.Sprintf("subcluster %s has invalid type change", v.Spec.Subclusters[inx].Name))
		allErrs = append(allErrs, err)
	}
	// Go through new object to see that existing subclusters have a valid type change.
	for i := range v.Spec.Subclusters {
		sc := v.Spec.Subclusters[i]
		oldType, ok := nameToTypeMap[sc.Name]
		// Can skip new subclusters
		if !ok {
			continue
		}
		if (oldType == PrimarySubcluster || oldType == TransientSubcluster) && oldType != sc.Type {
			invalidStateTransitionErr(i)
		} else if oldType == SecondarySubcluster && sc.Type != SecondarySubcluster {
			_, found := scToSbMap[sc.Name]
			// You can only transition out of a secondary subcluster if its during sandboxing.
			if sc.Type != SandboxPrimarySubcluster || !found {
				invalidStateTransitionErr(i)
			}
		}
	}
	return allErrs
}

func (v *VerticaDB) validateVerticaDBSpec() field.ErrorList {
	allErrs := v.hasAtLeastOneSC(field.ErrorList{})
	allErrs = v.hasValidSubclusterTypes(allErrs)
	allErrs = v.hasValidInitPolicy(allErrs)
	allErrs = v.hasValidRestorePolicy(allErrs)
	allErrs = v.hasValidSaveRestorePointConfig(allErrs)
	allErrs = v.hasValidDBName(allErrs)
	allErrs = v.hasPrimarySubcluster(allErrs)
	allErrs = v.validateKsafety(allErrs)
	allErrs = v.validateCommunalPath(allErrs)
	allErrs = v.validateS3ServerSideEncryption(allErrs)
	allErrs = v.validateAdditionalConfigParms(allErrs)
	allErrs = v.validateCustomLabels(allErrs)
	allErrs = v.validateIncludeUIDInPathAnnotation(allErrs)
	allErrs = v.validateEndpoint(allErrs)
	allErrs = v.hasValidSvcAndScName(allErrs)
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
	allErrs = v.hasValidShardCount(allErrs)
	allErrs = v.hasValidProbeOverrides(allErrs)
	allErrs = v.hasValidPodSecurityContext(allErrs)
	allErrs = v.hasValidNMAResourceLimit(allErrs)
	allErrs = v.hasValidCreateDBTimeout(allErrs)
	allErrs = v.hasValidUpgradePolicy(allErrs)
	allErrs = v.hasValidReplicaGroups(allErrs)
	allErrs = v.validateVersionAnnotation(allErrs)
	allErrs = v.validateSandboxes(allErrs)
	allErrs = v.checkNewSBoxOrSClusterShutdownUnset(allErrs)
	allErrs = v.validateProxyConfig(allErrs)
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

func (v *VerticaDB) hasValidSubclusterTypes(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.Type == PrimarySubcluster || sc.Type == SecondarySubcluster ||
			sc.Type == TransientSubcluster || sc.Type == SandboxPrimarySubcluster {
			continue
		}
		fieldPrefix := field.NewPath("spec").Child("subclusters").Index(i)
		err := field.Invalid(fieldPrefix.Child("type"),
			sc.Type,
			fmt.Sprintf("subcluster type is invalid. A valid case-sensitive type a user can specify is %q or %q. "+
				"(%q and %q are valid types that should only be set by the operator)",
				PrimarySubcluster, SecondarySubcluster, SandboxPrimarySubcluster, TransientSubcluster))
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

func (v *VerticaDB) hasValidRestorePolicy(allErrs field.ErrorList) field.ErrorList {
	if v.IsRestoreDuringReviveEnabled() && !v.Spec.RestorePoint.IsValidRestorePointPolicy() {
		if v.Spec.RestorePoint.Archive == "" {
			err := field.Invalid(field.NewPath("spec").Child("restorePoint"),
				v.Spec.RestorePoint,
				fmt.Sprintf("restorePoint is invalid. When initPolicy is set to %s and restorePoint is specified, "+
					"archive must be specified.", CommunalInitPolicyRevive))
			allErrs = append(allErrs, err)
		}
		commonErrorMessage := fmt.Sprintf("restorePoint is invalid. When initPolicy is set to %s and restorePoint is specified, "+
			"the database will initialize by reviving from a restore point in the specified archive, and thus "+
			"either restorePoint.index or restorePoint.id must be properly specified. ", CommunalInitPolicyRevive)
		if v.Spec.RestorePoint.Index <= 0 && v.Spec.RestorePoint.ID == "" {
			err := field.Invalid(field.NewPath("spec").Child("restorePoint"),
				v.Spec.RestorePoint,
				commonErrorMessage+"Both fields are currently empty or invalid.")
			allErrs = append(allErrs, err)
		} else if v.Spec.RestorePoint.Index > 0 && v.Spec.RestorePoint.ID != "" {
			err := field.Invalid(field.NewPath("spec").Child("restorePoint"),
				v.Spec.RestorePoint,
				commonErrorMessage+"Both fields are currently specified, which is not allowed.")
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) hasValidSaveRestorePointConfig(allErrs field.ErrorList) field.ErrorList {
	if v.IsSaveRestorepointEnabled() && !v.Spec.RestorePoint.IsValidForSaveRestorePoint() {
		err := field.Invalid(field.NewPath("spec").Child("restorePoint"),
			v.Spec.RestorePoint,
			"restorePoint is invalid. When save restore point is enabled, "+
				"archive must be specified.")
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

func (v *VerticaDB) validateIncludeUIDInPathAnnotation(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.InitPolicy == CommunalInitPolicyRevive &&
		v.IncludeUIDInPath() {
		prefix := field.NewPath("metadata").Child("annotations")
		annotationName := vmeta.IncludeUIDInPathAnnotation
		err := field.Invalid(prefix.Key(annotationName),
			v.Annotations[annotationName],
			fmt.Sprintf("%s must always be false when reviving a db", annotationName))
		allErrs = append(allErrs, err)
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
	// An empty endpoint is allowed. This lets the server pick a suitable
	// default based on the SDK that is used.
	if v.Spec.Communal.Endpoint == "" {
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
		if sc.IsPrimary() {
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
	switch v.IsKSafety0() {
	case true:
		if sizeSum < KSafety0MinHosts || sizeSum > KSafety0MaxHosts {
			err := field.Invalid(field.NewPath("annotations").Child(vmeta.KSafetyAnnotation),
				v.Annotations[vmeta.KSafetyAnnotation],
				fmt.Sprintf("with kSafety 0, the total size of the cluster must have between %d and %d hosts", KSafety0MinHosts, KSafety0MaxHosts))
			allErrs = append(allErrs, err)
		}
	case false:
		if sizeSum < KSafety1MinHosts {
			err := field.Invalid(field.NewPath("annotations").Child(vmeta.KSafetyAnnotation),
				v.Annotations[vmeta.KSafetyAnnotation],
				fmt.Sprintf("with kSafety 1, the total size of the cluster must have at least %d hosts", KSafety1MinHosts))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) getClusterSize() int {
	sizeSum := 0

	if v.IsKSafetyCheckStrict() {
		// in case the k-safety check is not relaxed,
		// we calculate the cluster size on the primary nodes only
		for i := range v.Spec.Subclusters {
			sc := &v.Spec.Subclusters[i]
			if sc.IsPrimary() && !sc.IsSandboxPrimary() {
				sizeSum += int(sc.Size)
			}
		}
	} else {
		for i := range v.Spec.Subclusters {
			sc := &v.Spec.Subclusters[i]
			sizeSum += int(sc.Size)
		}
	}

	return sizeSum
}

func (v *VerticaDB) hasValidSvcAndScName(allErrs field.ErrorList) field.ErrorList {
	// check headless svc names
	vdbName := v.Name
	// vdb name will be used as is for headless svc name,
	// thus match regex for service name (DNS-1035 label)
	isValidHlSvcName := IsValidServiceName(vdbName)
	if !isValidHlSvcName {
		err := field.Invalid(field.NewPath("metadata").Child("name"),
			v.ObjectMeta.Name,
			fmt.Sprintf("vdb name must match regex '%s'",
				RFC1035DNSLabelNameRegex))
		allErrs = append(allErrs, err)
	}
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		fieldPrefix := field.NewPath("spec").Child("subclusters").Index(i)
		stsName := sc.GetStatefulSetName(v)
		// check subcluster name
		if !IsValidSubclusterName(stsName) {
			err := field.Invalid(fieldPrefix.Child("name"),
				stsName,
				fmt.Sprintf("subcluster name is not valid, change it so that 1. its length is greater than 0 and smaller than 254,"+
					" 2. it matches regex '%s'", RFC1123DNSSubdomainNameRegex))
			allErrs = append(allErrs, err)
		}
		if !isValidHlSvcName {
			// The actual name of the headless service object is always prefixed with the name of the owning vdb,
			// so we skip headless service names check if vdb name is not valid to minimize confusion and avoid overwhelming error messages.
			continue
		}
		// check external svc names
		extSvcName := vdbName + "-" + sc.GetServiceName()
		// vdb name will be used together with subcluster serviceName for external svc name,
		// thus match regex for service name (DNS-1035 label)
		if !IsValidServiceName(extSvcName) {
			errMsg := fmt.Sprintf("subcluster serviceName (either user-provided or auto-generated by operator when omitted by user)"+
				" is prefixed by vdb name %q and \"-\" to generate external service name %q,"+
				" which must match regex '%s'; to fix, consider 1. change the subcluster name if serviceName is not explicitly provided,"+
				" 2. change the subcluster serviceName if it is explicitly provided, 3. change the vdb name",
				v.ObjectMeta.Name, extSvcName, RFC1035DNSLabelNameRegex)
			err := field.Invalid(fieldPrefix.Child("serviceName"),
				sc.GetServiceName(),
				errMsg)
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
			if isNodePortNumberInvalid(sc.ClientNodePort) {
				allErrs = v.genNodePortInvalidError(allErrs, pathPrefix,
					"clientNodePort", v.Spec.Subclusters[i].ClientNodePort)
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
			if sc.ClientNodePort != 0 {
				err := field.Invalid(field.NewPath("spec").Child("subclusters").Index(i).Child("clientNodePort"),
					v.Spec.Subclusters[i].ClientNodePort,
					fmt.Sprintf("clientNodePort can only be specified for service types %s and %s",
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
			// check the statefulset name override annotation for each subcluster.
			if sc1.GetStatefulSetName(v) == sc2.GetStatefulSetName(v) {
				err := field.Invalid(field.NewPath("spec").Child("subclusters").Index(j).
					Child("annotations").Key(vmeta.StsNameOverrideAnnotation),
					v.Spec.Subclusters[j].Annotations[vmeta.StsNameOverrideAnnotation],
					fmt.Sprintf("duplicates the %s annotation of subcluster[%d]", vmeta.StsNameOverrideAnnotation, i))
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
	if (v.GetKerberosRealm() == "" && v.GetKerberosServiceName() == "") ||
		(v.GetKerberosRealm() != "" && v.GetKerberosServiceName() != "" && v.Spec.KerberosSecret != "") {
		return allErrs
	}

	prefix := field.NewPath("spec").Child("communal").Child("additionalConfig")
	if v.GetKerberosRealm() == "" {
		err := field.Invalid(prefix.Key(vmeta.KerberosRealmConfig),
			v.GetKerberosRealm(),
			`communal.additionalConfig["kerberosRealm"] must be set if setting up Kerberos`)
		allErrs = append(allErrs, err)
	}
	if v.GetKerberosServiceName() == "" {
		err := field.Invalid(prefix.Key(vmeta.KerberosServiceNameConfig),
			v.GetKerberosServiceName(),
			`communal.additionalConfig["kerberosServiceName"] must be set if setting up Kerberos`)
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
	if v.Spec.TemporarySubclusterRouting == nil {
		return allErrs
	}
	scMap := v.GenSubclusterMap()
	fieldPrefix := field.NewPath("spec").Child("temporarySubclusterRouting")
	if v.Spec.TemporarySubclusterRouting.Template.Name != "" {
		templateFieldPrefix := fieldPrefix.Child("template")
		if !v.Spec.TemporarySubclusterRouting.Template.IsSecondary() {
			err := field.Invalid(templateFieldPrefix.Child("type"),
				v.Spec.TemporarySubclusterRouting.Template.Type,
				"subcluster template must be a secondary subcluster")
			allErrs = append(allErrs, err)
		}
		if v.Spec.TemporarySubclusterRouting.Template.Size == 0 {
			err := field.Invalid(templateFieldPrefix.Child("size"),
				v.Spec.TemporarySubclusterRouting.Template.Size,
				"size of subcluster template must be greater than zero")
			allErrs = append(allErrs, err)
		}
		if sc, ok := scMap[v.Spec.TemporarySubclusterRouting.Template.Name]; ok && !sc.IsTransient() {
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
				if sc.ClientNodePort != osc.ClientNodePort {
					err := field.Invalid(fieldPrefix.Child("clientNodePort").Index(i),
						sc.ClientNodePort,
						"clientNodePort doesn't match other subcluster(s) sharing the same serviceName")
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
		if !sc.IsTransient() {
			continue
		}

		fieldPrefix := field.NewPath("spec").Child("subclusters").Index(i)
		if v.Spec.TemporarySubclusterRouting != nil && sc.Name != v.Spec.TemporarySubclusterRouting.Template.Name {
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
	prefix := field.NewPath("metadata").Child("annotations")
	if v.GetRequeueTime() < 0 {
		err := field.Invalid(prefix.Key(vmeta.RequeueTimeAnnotation),
			v.Annotations[vmeta.RequeueTimeAnnotation],
			"requeue time cannot be negative")
		allErrs = append(allErrs, err)
	}
	if v.GetUpgradeRequeueTime() < 0 {
		err := field.Invalid(prefix.Key(vmeta.UpgradeRequeueTimeAnnotation),
			v.Annotations[vmeta.UpgradeRequeueTimeAnnotation],
			"upgrade requeue time cannot be negative")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) validateEncryptSpreadComm(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.EncryptSpreadComm != "" && v.Spec.EncryptSpreadComm != EncryptSpreadCommDisabled &&
		v.Spec.EncryptSpreadComm != EncryptSpreadCommWithVertica {
		err := field.Invalid(field.NewPath("spec").Child("encrpytSpreadComm"),
			v.Spec.EncryptSpreadComm,
			fmt.Sprintf("encryptSpreadComm can only be set to an empty string, %s, or %s",
				EncryptSpreadCommWithVertica, EncryptSpreadCommDisabled))
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

func (v *VerticaDB) hasValidPodSecurityContext(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.PodSecurityContext == nil {
		return allErrs
	}

	const RootUIDVal = 0
	rootUID := int64(RootUIDVal)
	if v.Spec.PodSecurityContext.RunAsUser != nil && *v.Spec.PodSecurityContext.RunAsUser == rootUID {
		err := field.Invalid(field.NewPath("spec").Child("podSecurityContext").Child("runAsUser"),
			v.Spec.PodSecurityContext.RunAsUser,
			"cannot run vertica pods as root (uid == 0)")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) hasValidNMAResourceLimit(allErrs field.ErrorList) field.ErrorList {
	nmaMemoryLimit := vmeta.GetNMAResource(v.Annotations, v1.ResourceLimitsMemory)
	if nmaMemoryLimit.IsZero() { // Zero implies it isn't set.
		return allErrs
	}
	if vmeta.MinNMAMemoryLimit.Cmp(nmaMemoryLimit) == 1 {
		annotationName := vmeta.GenNMAResourcesAnnotationName(v1.ResourceLimitsMemory)
		err := field.Invalid(field.NewPath("metadata").Child("annotations").Child(annotationName),
			nmaMemoryLimit, fmt.Sprintf("cannot be less than %s", vmeta.MinNMAMemoryLimit.String()))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) hasValidCreateDBTimeout(allErrs field.ErrorList) field.ErrorList {
	createDBTimeout := v.GetCreateDBNodeStartTimeout()
	if createDBTimeout < 0 {
		annotationName := vmeta.CreateDBTimeoutAnnotation
		err := field.Invalid(field.NewPath("metadata").Child("annotations").Child(annotationName),
			createDBTimeout, fmt.Sprintf("%s must be non-negative", annotationName))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) hasValidUpgradePolicy(allErrs field.ErrorList) field.ErrorList {
	switch v.Spec.UpgradePolicy {
	case "":
	case AutoUpgrade:
	case OfflineUpgrade:
	case OnlineUpgrade:
	case ReadOnlyOnlineUpgrade:
	default:
		err := field.Invalid(field.NewPath("spec").Child("upgradePolicy"),
			v.Spec.UpgradePolicy, fmt.Sprintf("must be one of: %s, %s, %s or %s",
				AutoUpgrade, OfflineUpgrade, OnlineUpgrade, ReadOnlyOnlineUpgrade))
		return append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) hasValidReplicaGroups(allErrs field.ErrorList) field.ErrorList {
	// Can be skipped if Online upgrade is not in progress
	if !v.isOnlineUpgradeInProgress() {
		return allErrs
	}

	// We verify that the replica group for each subcluster is valid
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if val, found := sc.Annotations[vmeta.ReplicaGroupAnnotation]; found {
			if val != vmeta.ReplicaGroupAValue && val != vmeta.ReplicaGroupBValue && val != "" {
				err := field.Invalid(
					field.NewPath("spec").Child("subclusters").Index(i).Child("annotations").Key(vmeta.ReplicaGroupAnnotation),
					val,
					fmt.Sprintf("subcluster %q has an invalid value %q for the annotation %q",
						sc.Name, val, vmeta.ReplicaGroupAnnotation))
				allErrs = append(allErrs, err)
			}
		}
	}
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

// validateVersionAnnotation validates if the version annotation has a correct format
func (v *VerticaDB) validateVersionAnnotation(allErrs field.ErrorList) field.ErrorList {
	vdbVer, ok := v.GetVerticaVersionStr()
	if ok {
		_, err := vversion.MakeInfoFromStrCheck(vdbVer)
		prefix := field.NewPath("metadata").Child("annotations")
		if err != nil {
			err := field.Invalid(prefix.Key(vmeta.VersionAnnotation),
				v.Annotations[vmeta.VersionAnnotation],
				err.Error())
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// validateSandboxes validates if provided sandboxes info is correct
func (v *VerticaDB) validateSandboxes(allErrs field.ErrorList) field.ErrorList {
	// if vdb does not have any sandboxes, skip this check
	if len(v.Spec.Sandboxes) == 0 {
		return allErrs
	}

	sandboxes := v.Spec.Sandboxes
	seenSandbox := make(map[string]any)
	mainClusterImage := v.Spec.Image
	path := field.NewPath("spec").Child("sandboxes")
	for i, sandbox := range sandboxes {
		// check if we have empty sandbox names
		if sandbox.Name == "" {
			err := field.Invalid(path.Index(i),
				sandboxes[i],
				"sandbox name cannot be empty")
			allErrs = append(allErrs, err)
		}
		// check if sandbox name matches rfc 1123 regex
		if !isValidRFC1123DNSSubdomainName(GenCompatibleFQDNHelper(sandbox.Name)) {
			err := field.Invalid(path.Index(i),
				sandboxes[i],
				fmt.Sprintf("sandbox name must match regex '%s'",
					RFC1123DNSSubdomainNameRegex))
			allErrs = append(allErrs, err)
		}
		// check if we have duplicate sandboxes
		if _, ok := seenSandbox[sandbox.Name]; ok {
			err := field.Invalid(path.Index(i),
				sandboxes[i],
				fmt.Sprintf("sandbox %s already exists", sandbox.Name))
			allErrs = append(allErrs, err)
		}
		seenSandbox[sandbox.Name] = struct{}{}
		// check if sandbox image is different than main cluster's before sandbox has been setup
		if sandbox.Image != "" && sandbox.Image != mainClusterImage && !v.isUpgradeInProgress() && !v.isSandboxInitialized(sandbox.Name) {
			err := field.Invalid(path.Index(i),
				sandboxes[i],
				fmt.Sprintf("sandbox %s cannot have a different image than the main cluster before its creation", sandbox.Name))
			allErrs = append(allErrs, err)
		}
	}

	// check if we are using a vertica version older than v24.3.0
	vdbVer, ok := v.GetVerticaVersionStr()
	prefix := field.NewPath("metadata").Child("annotations")
	if ok {
		verInfo, err := vversion.MakeInfoFromStrCheck(vdbVer)
		if err == nil && verInfo.IsOlder(SandboxSupportedMinVersion) {
			err := field.Invalid(prefix.Key(vmeta.VersionAnnotation),
				v.Annotations[vmeta.VersionAnnotation],
				fmt.Sprintf("sandbox is unsupported in version %s. A minimum version of %s is required", vdbVer, SandboxSupportedMinVersion))
			allErrs = append(allErrs, err)
		}
	}

	// check if we are using vclusterOps deployments
	if !vmeta.UseVClusterOps(v.Annotations) {
		err := field.Invalid(prefix.Key(vmeta.VClusterOpsAnnotation),
			v.Annotations[vmeta.VClusterOpsAnnotation],
			"sandbox is unsupported for admintools deployments")
		allErrs = append(allErrs, err)
	}

	return v.validateSubclustersInSandboxes(allErrs)
}

// validateProxyConfig validates if provided proxy info is correct
func (v *VerticaDB) validateProxyConfig(allErrs field.ErrorList) field.ErrorList {
	if !vmeta.UseVProxy(v.Annotations) {
		return allErrs
	}
	allErrs = v.validateSpecProxy(allErrs)
	allErrs = v.validateSubclusterProxy(allErrs)
	return v.validateProxyLogLevel(allErrs)
}

// validateSpecProxy checks if proxy set and image must be non-empty
func (v *VerticaDB) validateSpecProxy(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.Proxy == nil {
		err := field.Invalid(field.NewPath("spec").Child("proxy"),
			"",
			"proxy is not set")
		allErrs = append(allErrs, err)
	}
	if v.Spec.Proxy != nil && v.Spec.Proxy.Image == "" {
		err := field.Invalid(field.NewPath("spec").Child("proxy").Child("image"),
			v.Spec.Proxy.Image,
			"proxy.image cannot be empty")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// validateSubclusterProxy checks if replica is set and value is valid
func (v *VerticaDB) validateSubclusterProxy(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		// proxy replicas must be > 0(at all times)
		if sc.Proxy != nil && sc.Proxy.Replicas != nil && *sc.Proxy.Replicas <= 0 {
			err := field.Invalid(
				field.NewPath("spec").Child("subclusters").Index(i).Child("proxy").Child("replicas"),
				sc.Proxy.Replicas,
				fmt.Sprintf("subcluster %q should have a positive number for proxy replica",
					sc.Name))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// validateProxyLogLevel checks if log level annotation value valid
func (v *VerticaDB) validateProxyLogLevel(allErrs field.ErrorList) field.ErrorList {
	proxyLogLevel := vmeta.GetVProxyLogLevel(v.Annotations)
	if !slices.Contains(validProxyLogLevel, proxyLogLevel) {
		prefix := field.NewPath("metadata").Child("annotations")
		errMsg := fmt.Sprintf("annotation %s value is not valid, please use one of the following values %v",
			vmeta.VProxyLogLevelAnnotation, validProxyLogLevel)
		err := field.Invalid(prefix.Key(vmeta.VProxyLogLevelAnnotation),
			v.Annotations[vmeta.VProxyLogLevelAnnotation],
			errMsg)
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// validateSandboxes validates if subclusters in sandboxes is correct
func (v *VerticaDB) validateSubclustersInSandboxes(allErrs field.ErrorList) field.ErrorList {
	sandboxes := v.Spec.Sandboxes
	seenScWithSbIndex := make(map[string]int)
	path := field.NewPath("spec").Child("sandboxes")
	for i, sandbox := range sandboxes {
		scInSandbox := make(map[string]int)
		for _, sc := range sandbox.Subclusters {
			// check if we have duplicate subclusters in a sandbox
			if _, ok := scInSandbox[sc.Name]; ok {
				err := field.Invalid(path.Index(i),
					sandboxes[i],
					fmt.Sprintf("found duplicate subcluster %s in sandbox %s", sc.Name, sandbox.Name))
				allErrs = append(allErrs, err)
			}
			// check if a subcluster is defined in multiple sandboxes
			if _, ok := seenScWithSbIndex[sc.Name]; ok {
				err := field.Invalid(path.Index(i),
					sandboxes[i],
					fmt.Sprintf("subcluster %s already exists in another sandbox", sc.Name))
				allErrs = append(allErrs, err)
			}
			scInSandbox[sc.Name] = i
		}
		for sc, index := range scInSandbox {
			seenScWithSbIndex[sc] = index
		}
	}

	// check if a non-existing subcluster is defined in a sandbox
	scMap := v.GenSubclusterMap()
	for sc, i := range seenScWithSbIndex {
		if scInfo, ok := scMap[sc]; !ok {
			err := field.Invalid(path.Index(i),
				sandboxes[i],
				fmt.Sprintf("subcluster %s does not exist", sc))
			allErrs = append(allErrs, err)
		} else if scInfo.IsMainPrimary() {
			err := field.Invalid(path.Index(i),
				sandboxes[i],
				fmt.Sprintf("subcluster %s is a primary subcluster that is not allowed to be in a sandbox", sc))
			allErrs = append(allErrs, err)
		}
	}

	return allErrs
}

func (v *VerticaDB) isUpgradeInProgress() bool {
	return v.IsStatusConditionTrue(UpgradeInProgress)
}

func (v *VerticaDB) isOnlineUpgradeInProgress() bool {
	return v.IsStatusConditionTrue(OnlineUpgradeInProgress)
}

func (v *VerticaDB) isDBInitialized() bool {
	return v.IsStatusConditionTrue(DBInitialized)
}

// isSandboxInitialized returns ture when the sandbox has been created in the database
func (v *VerticaDB) isSandboxInitialized(targetSb string) bool {
	for _, sb := range v.Status.Sandboxes {
		if sb.Name == targetSb {
			return true
		}
	}
	return false
}

func (v *VerticaDB) checkImmutableBasic(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
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
	// when update subcluster names, there should be at least one sc's name match its old name.
	// This limitation should not be hold in online upgrade since we need to rename all subclusters
	// after sandbox promotion.
	if !v.canUpdateScName(oldObj) && !v.isOnlineUpgradeInProgress() {
		err := field.Invalid(field.NewPath("spec").Child("subclusters"),
			v.Spec.Subclusters,
			"at least one subcluster name should match its old name")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// checkImmutableUpgradePolicy will see if it unsafe to change the
// upgradePolicy.  It will log an error if it detects a change in that field
// when it isn't allowed.
func (v *VerticaDB) checkImmutableUpgradePolicy(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	if v.Spec.UpgradePolicy == oldObj.Spec.UpgradePolicy ||
		!oldObj.isUpgradeInProgress() {
		return allErrs
	}
	err := field.Invalid(field.NewPath("spec").Child("upgradePolicy"),
		v.Spec.UpgradePolicy,
		"upgradePolicy cannot change because upgrade is in progress")
	allErrs = append(allErrs, err)
	return allErrs
}

// checkImmutableDeploymentMethod will check if the deployment type is changing from
// vclusterops to admintools, which isn't allowed.
func (v *VerticaDB) checkImmutableDeploymentMethod(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	if vmeta.UseVClusterOps(oldObj.Annotations) && !vmeta.UseVClusterOps(v.Annotations) {
		// change from vclusterops deployment to admintools deployment
		prefix := field.NewPath("metadata").Child("annotations")
		err := field.Invalid(prefix.Key(vmeta.VClusterOpsAnnotation),
			v.Annotations[vmeta.VClusterOpsAnnotation],
			"deployment type cannot change from vclusterops to admintools")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// checkSandboxesDuringUpgrade will check if sandboxes size has changed during an upgrade.
func (v *VerticaDB) checkSandboxesDuringUpgrade(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// No error if upgrade is not in progress
	if !oldObj.isUpgradeInProgress() {
		return allErrs
	}
	// No error if sandboxes size did not change
	if len(v.Spec.Sandboxes) == len(oldObj.Spec.Sandboxes) {
		return allErrs
	}
	upgradeSbName := vmeta.GetOnlineUpgradeSandbox(v.Annotations)
	// No error if the sandbox changed is used by online upgrade.
	if (len(v.Spec.Sandboxes) == 1 && v.Spec.Sandboxes[0].Name == upgradeSbName) ||
		(len(v.Spec.Sandboxes) == 0 && oldObj.Spec.Sandboxes[0].Name == upgradeSbName) {
		return allErrs
	}
	err := field.Invalid(field.NewPath("spec").Child("sandboxes"),
		v.Spec.Sandboxes,
		"cannot add or remove sandboxes during an upgrade")
	return append(allErrs, err)
}

// checkImmutableTemporarySubclusterRouting will check if
// temporarySubclusterRouting is changing when it isn't allowed to.
func (v *VerticaDB) checkImmutableTemporarySubclusterRouting(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// TemporarySubclusterRouting is allowed to change as long as an image
	// change isn't in progress
	if !oldObj.isUpgradeInProgress() {
		return allErrs
	}
	if v.Spec.TemporarySubclusterRouting == nil && oldObj.Spec.TemporarySubclusterRouting == nil {
		return allErrs
	}
	fieldPrefix := field.NewPath("spec").Child("temporarySubclusterRouting")
	if v.Spec.TemporarySubclusterRouting == nil && oldObj.Spec.TemporarySubclusterRouting != nil {
		err := field.Invalid(fieldPrefix,
			v.Spec.TemporarySubclusterRouting,
			"cannot clear the temporarySubclusterRouting field during an upgrade")
		return append(allErrs, err)
	}
	if v.Spec.TemporarySubclusterRouting != nil && oldObj.Spec.TemporarySubclusterRouting == nil {
		err := field.Invalid(fieldPrefix,
			v.Spec.TemporarySubclusterRouting,
			"cannot set the temporarySubclusterRouting field during an upgrade")
		return append(allErrs, err)
	}
	if !reflect.DeepEqual(v.Spec.TemporarySubclusterRouting.Names, oldObj.Spec.TemporarySubclusterRouting.Names) {
		err := field.Invalid(fieldPrefix.Child("names"),
			v.Spec.TemporarySubclusterRouting.Names,
			"subcluster names for temporarySubclusterRouting cannot change when an upgrade is in progress")
		allErrs = append(allErrs, err)
	}
	if !reflect.DeepEqual(v.Spec.TemporarySubclusterRouting.Template, oldObj.Spec.TemporarySubclusterRouting.Template) {
		err := field.Invalid(fieldPrefix.Child("template"),
			v.Spec.TemporarySubclusterRouting.Template,
			"template for temporarySubclusterRouting cannot change when an upgrade is in progress")
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

func (v *VerticaDB) checkImmutablePodSecurityContext(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// PodSecurityContext can change if we haven't yet created/revived the database
	if !v.isDBInitialized() {
		return allErrs
	}

	prefix := field.NewPath("spec").Child("podSecurityContext")

	oldPsc := oldObj.Spec.PodSecurityContext
	newPsc := v.Spec.PodSecurityContext
	if (oldPsc == nil && newPsc != nil) || (oldPsc != nil && newPsc == nil) {
		if oldPsc != nil && (oldPsc.RunAsUser != nil || oldPsc.FSGroup != nil) {
			err := field.Invalid(prefix,
				newPsc,
				"Cannot clear the runAsUser or fsGroup settings after DB initialization")
			return append(allErrs, err)
		}
		if newPsc != nil && (newPsc.RunAsUser != nil || newPsc.FSGroup != nil) {
			err := field.Invalid(prefix,
				newPsc,
				"Cannot set the runAsUser or fsGroup settings after DB initialization")
			return append(allErrs, err)
		}
	}
	if oldPsc == nil {
		return allErrs
	}

	allErrs = checkInt64PtrChange(prefix, "fsGroup", oldPsc.FSGroup, newPsc.FSGroup, allErrs)
	allErrs = checkInt64PtrChange(prefix, "runAsUser", oldPsc.RunAsUser, newPsc.RunAsUser, allErrs)
	return allErrs
}

func checkInt64PtrChange(prefix *field.Path, fieldName string,
	oldVal, newVal *int64, allErrs field.ErrorList) field.ErrorList {
	path := prefix.Child(fieldName)
	if oldVal == nil && newVal != nil {
		err := field.Invalid(path,
			newVal,
			fmt.Sprintf("Cannot set %s after DB initialization", fieldName))
		return append(allErrs, err)
	}
	if oldVal != nil && newVal == nil {
		err := field.Invalid(path,
			newVal,
			fmt.Sprintf("Cannot clear %s after DB initialization", fieldName))
		return append(allErrs, err)
	}
	if *oldVal != *newVal {
		err := field.Invalid(path,
			newVal,
			fmt.Sprintf("Cannot change %s after DB initialization", fieldName))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// checkImmutableSubclusterDuringUpgrade will ensure we don't scale, add or
// remove subclusters during a online upgrade.
func (v *VerticaDB) checkImmutableSubclusterDuringUpgrade(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// This entire check can be skipped if we aren't doing online upgrade.
	if !v.isOnlineUpgradeInProgress() {
		return allErrs
	}

	// The subclusters that are taking part in the upgrade must stay constant
	// during the upgrade. The upgrade itself may add new subclusters, but it
	// has to be annotated a certain way. Regular users should not add new
	// subclusters.

	// Come up with a combined list of all of the subclusters. This will include
	// all subclusters that are being removed and added.
	oldScMap := oldObj.GenSubclusterMap()
	newScMap := v.GenSubclusterMap()
	allSubclusters := map[string]bool{}
	for scName := range oldScMap {
		allSubclusters[scName] = true
	}
	for scName := range newScMap {
		allSubclusters[scName] = true
	}

	path := field.NewPath("spec").Child("subclusters")
	for scName := range allSubclusters {
		oldSc, oldScFound := oldScMap[scName]
		newSc, newScFound := newScMap[scName]
		if !newScFound {
			continue
		}
		// The operator can create new subclusters that are used for the
		// upgrade process. But these must be secondary subclusters and have the
		// replica group annotation.
		if !oldScFound {
			annotationVal, annotationFound := newSc.Annotations[vmeta.ReplicaGroupAnnotation]
			if !annotationFound ||
				(annotationVal != vmeta.ReplicaGroupAValue && annotationVal != vmeta.ReplicaGroupBValue) {
				err := field.Invalid(path,
					newSc,
					"New subclusters cannot be added during online upgrade")
				allErrs = append(allErrs, err)
			}
			continue
		}
		if newSc.Size != oldSc.Size {
			err := field.Invalid(path,
				newSc,
				fmt.Sprintf("Cannot change size of subcluster %q while database is being upgraded", scName))
			allErrs = append(allErrs, err)
			continue
		}
	}

	return allErrs
}

// findPersistScsInSandbox returns the sandboxed subclusters that exist in both old and new vdb with their sandbox indexes
func (v *VerticaDB) findPersistScsInSandbox(oldObj *VerticaDB) map[string]int {
	oldSandboxes := oldObj.Spec.Sandboxes
	newSandboxes := v.Spec.Sandboxes
	sandboxScMap := make(map[string][]SubclusterName)
	for _, sandbox := range oldSandboxes {
		sandboxScMap[sandbox.Name] = sandbox.Subclusters
	}
	persistScsWithSbIndex := make(map[string]int)
	for i, sandbox := range newSandboxes {
		if oldScs, ok := sandboxScMap[sandbox.Name]; ok {
			oldScMap := make(map[string]any)
			for _, oldSc := range oldScs {
				oldScMap[oldSc.Name] = struct{}{}
			}
			for _, newSc := range sandbox.Subclusters {
				if _, ok := oldScMap[newSc.Name]; ok {
					persistScsWithSbIndex[newSc.Name] = i
				}
			}
		}
	}
	return persistScsWithSbIndex
}

// findPersistSandboxes returns a slice of sandbox names that are in both new and old vdb.
func (v *VerticaDB) findPersistSandboxes(oldObj *VerticaDB) map[string]bool {
	oldSandboxes := oldObj.Spec.Sandboxes
	newSandboxes := v.Spec.Sandboxes
	oldSandboxMap := make(map[string]bool)
	for _, oldSandbox := range oldSandboxes {
		oldSandboxMap[oldSandbox.Name] = true
	}
	persistSandboxes := make(map[string]bool)
	for _, newSandbox := range newSandboxes {
		if _, ok := oldSandboxMap[newSandbox.Name]; ok {
			persistSandboxes[newSandbox.Name] = true
		}
	}
	return persistSandboxes
}

// checkImmutableSubclusterInSandbox ensures we do not scale and remove any subcluster that is in a sandbox
func (v *VerticaDB) checkImmutableSubclusterInSandbox(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// if either old vdb or new vdb does not have any sandboxes, skip this check
	if len(oldObj.Spec.Sandboxes) == 0 || len(v.Spec.Sandboxes) == 0 {
		return allErrs
	}

	persistScsWithSbIndex := v.findPersistScsInSandbox(oldObj)
	oldScIndexMap := oldObj.GenSubclusterIndexMap()
	newScIndexMap := v.GenSubclusterIndexMap()
	oldScMap := oldObj.GenSubclusterMap()
	newScMap := v.GenSubclusterMap()
	path := field.NewPath("spec").Child("subclusters")

	allErrs = v.checkSubclusterSizeInSandbox(allErrs, persistScsWithSbIndex, newScIndexMap, oldScMap,
		newScMap, path)
	allErrs = v.checkSandboxSubclustersRemoved(allErrs, oldObj, oldScIndexMap, oldScMap, newScMap, path)
	return v.checkSandboxPrimary(allErrs, oldObj, oldScIndexMap, oldScMap, path)
}

// checkSubclusterSizeInSandbox checks if the sizes of subclusters in a sandbox get changed
func (v *VerticaDB) checkSubclusterSizeInSandbox(allErrs field.ErrorList, persistScsWithSbIndex, newScIndexMap map[string]int,
	oldScMap, newScMap map[string]*Subcluster, path *field.Path) field.ErrorList {
	for sc, i := range persistScsWithSbIndex {
		oldSc, hasOldSc := oldScMap[sc]
		newSc, hasNewSc := newScMap[sc]
		if !hasOldSc || !hasNewSc {
			err := field.Invalid(path.Index(i),
				v.Spec.Sandboxes[i],
				fmt.Sprintf("Subcluster %s in vdb.spec.sandboxes cannot be found in vdb.spec.subclusters", sc))
			allErrs = append(allErrs, err)
			continue
		}
		if oldSc.Size != newSc.Size {
			index := newScIndexMap[sc]
			err := field.Invalid(path.Index(index),
				v.Spec.Subclusters[index],
				fmt.Sprintf("Cannot change the size of subcluster %q when it is in a sandbox", sc))
			allErrs = append(allErrs, err)
			continue
		}
	}
	return allErrs
}

// checkSandboxSubclustersRemoved checks subclusters that are sandboxed and removed in new vdb
func (v *VerticaDB) checkSandboxSubclustersRemoved(allErrs field.ErrorList, oldObj *VerticaDB, oldScIndexMap map[string]int,
	oldScMap, newScMap map[string]*Subcluster, path *field.Path) field.ErrorList {
	newScInSandbox := v.GenSubclusterSandboxMap()
	removedScs := vutil.MapKeyDiff(oldScMap, newScMap)
	for _, sc := range removedScs {
		if _, ok := newScInSandbox[sc]; !ok {
			continue
		}
		i := oldScIndexMap[sc]
		err := field.Invalid(path.Index(i),
			oldObj.Spec.Subclusters[i],
			fmt.Sprintf("Cannot remove subcluster %q when it is in a sandbox", sc))
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// checkSandboxPrimary ensures a couple of things:
//   - a sandbox primary subcluster cannot be moved to another sandbox
//   - cannot remove the sandbox primary subcluster from a sandbox
//
// The sandbox primary subcluster must stay constant until the sandbox is
// removed entirely.
func (v *VerticaDB) checkSandboxPrimary(allErrs field.ErrorList, oldObj *VerticaDB, oldScIndexMap map[string]int,
	oldScMap map[string]*Subcluster, path *field.Path) field.ErrorList {
	oldScInSandbox := oldObj.GenSubclusterSandboxMap()
	newScInSandbox := v.GenSubclusterSandboxMap()
	oldSbIndexMap := oldObj.GenSandboxIndexMap()
	oldSbMap := oldObj.GenSandboxMap()
	newSbMap := v.GenSandboxMap()
	for oldScName, oldSbName := range oldScInSandbox {
		sc := oldScMap[oldScName]

		// sandbox is removed
		if _, sandboxExist := newSbMap[oldSbName]; !sandboxExist {
			continue
		}

		newSbName, newFound := newScInSandbox[oldScName]
		// old sandbox still exits
		if !newFound && sc.Type == SandboxPrimarySubcluster {
			i := oldScIndexMap[oldScName]
			err := field.Invalid(path.Index(i),
				oldObj.Spec.Subclusters[i],
				fmt.Sprintf("Cannot remove primary subcluster %q from the sandbox", oldScName))
			allErrs = append(allErrs, err)
			continue
		}
		// old sandbox exists and subcluster either exists (primary or nonprimary) or removed (nonprimary)
		// Remaining check is concerned with subclusters moving between sandboxes.
		if oldSbName == newSbName {
			continue
		}
		// old sandbox name does not match new sandbox name
		if sc.Type == SandboxPrimarySubcluster {
			i := oldSbIndexMap[oldSbName]
			p := field.NewPath("spec").Child("sandboxes")
			err := field.Invalid(p.Index(i),
				oldSbMap[oldSbName],
				fmt.Sprintf("cannot remove the primary subcluster from sandbox %q unless you are removing the sandbox",
					oldScName))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// checkNewSBoxOrSClusterShutdownUnset ensures a new subcluster or sandbox to be added to a vdb has Shutdown field set to false
func (v *VerticaDB) checkNewSBoxOrSClusterShutdownUnset(allErrs field.ErrorList) field.ErrorList {
	statusSClusterMap := v.GenStatusSubclusterMap()
	statusSBoxMap := v.GenStatusSandboxMap()
	for i := range v.Spec.Sandboxes {
		newSBox := v.Spec.Sandboxes[i]
		if _, foundInStatus := statusSBoxMap[newSBox.Name]; !foundInStatus && newSBox.Shutdown {
			p := field.NewPath("spec").Child("sandboxes").Index(i).Child("shutdown")
			err := field.Invalid(p,
				newSBox.Shutdown,
				fmt.Sprintf("shutdown must be false when adding sandbox %q",
					newSBox.Name))
			allErrs = append(allErrs, err)
		}
	}
	for i := range v.Spec.Subclusters {
		newSCluster := &v.Spec.Subclusters[i]
		if _, foundInStatus := statusSClusterMap[newSCluster.Name]; !foundInStatus && newSCluster.Shutdown {
			p := field.NewPath("spec").Child("subclusters").Index(i).Child("shutdown")
			err := field.Invalid(p,
				newSCluster.Shutdown,
				fmt.Sprintf("shutdown must be false when adding subcluster %q",
					newSCluster.Name))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// checkShutdownForScaleOutOrIn ensures a subcluster to be scaled out/in has Shutdown field set to false
func (v *VerticaDB) checkShutdownForScaleOutOrIn(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	newSclusterMap := v.GenSubclusterMap()
	oldSclusterMap := oldObj.GenSubclusterMap()
	newSclusterIndexMap := v.GenSubclusterIndexMap()
	statusSubclusterMap := v.GenStatusSubclusterMap()
	sclusterSboxMap := v.GenSubclusterSandboxMap()
	for subclusterName, newScluster := range newSclusterMap {
		oldScluster, foundSclusterInOld := oldSclusterMap[subclusterName]
		if !foundSclusterInOld {
			continue // we only need to deal with persistent subclusters
		}
		_, inSandbox := sclusterSboxMap[subclusterName]
		if inSandbox {
			continue // this scenario is handled by checkImmutableSubclusterInSandbox()
		}
		if oldScluster.Size != newScluster.Size { // scale out/in
			subclusterStatus, hasStatus := statusSubclusterMap[subclusterName]
			if newScluster.Shutdown || (hasStatus && subclusterStatus.Shutdown) {
				i := newSclusterIndexMap[subclusterName]
				p := field.NewPath("spec").Child("subclusters").Index(i).Child("size")
				err := field.Invalid(p,
					newScluster.Size,
					fmt.Sprintf("cannot scale out/in %q which is marked for shutdown or has been shut down",
						subclusterName))
				allErrs = append(allErrs, err)
			}
		}
	}
	return allErrs
}

// checkSClusterToBeSandboxedShutdownUnset ensures the following when a subcluster (A) is to be added to a sandbox (B)
// 1 A's Shutdown field is set to false
// 2 B's Shutdown field is set to false
// 3 B does not have any subcluster in it that has Shutdown set to true (spec/status)
func (v *VerticaDB) checkSClusterToBeSandboxedShutdownUnset(allErrs field.ErrorList) field.ErrorList {
	newSclusterMap := v.GenSubclusterMap()
	statusScluterSboxMap := v.GenSubclusterSandboxStatusMap()
	newSclusterSboxMap := v.GenSubclusterSandboxMap()
	newSandboxIndexMap := v.GenSandboxIndexMap()
	newSandboxMap := v.GenSandboxMap()
	statusSclusterIndexMap := v.GenStatusSClusterIndexMap()
	sandboxWithError := map[string]bool{}
	for newSC, newSB := range newSclusterSboxMap {
		oldSandboxName, isInOldSandbox := statusScluterSboxMap[newSC]
		_, foundSubcluster := newSclusterMap[newSC]
		if !foundSubcluster {
			continue // avoid panic. this error should have been reported by other functions
		}
		// the subcluster to be sandboxed is either a new subcluster to be added (not found in status)
		// or an existing subcluster (found in status), the latter of which includes two scenarios:
		//   1 the subcluster was not in a sandbox previously
		//   2 the subcluster was not in a sandbox previously
		if !isInOldSandbox || (isInOldSandbox && oldSandboxName != newSB) {
			sandbox := newSandboxMap[newSB]
			errMsgs := v.checkSboxForShutdown(sandbox, newSclusterMap, statusSclusterIndexMap)
			if len(errMsgs) != 0 {
				sandboxIndex := newSandboxIndexMap[newSB]
				_, found := sandboxWithError[newSB]
				if !found {
					sandboxWithError[newSB] = true
					sclusterIndex := v.findSubclusterIndexInSandbox(newSC, sandbox)
					p := field.NewPath("spec").Child("sandboxes").Index(sandboxIndex).Child("subclusters").Index(sclusterIndex)
					err := field.Invalid(p,
						newSC,
						fmt.Sprintf("cannot sandbox subcluster %q in sandbox %q because %q",
							newSC, newSB, strings.Join(errMsgs, ",")))
					allErrs = append(allErrs, err)
				}
			}
		}
	}
	return allErrs
}

// findSclustersToUnsandbox return a map whose key is the pointer of a subcluster that is to be unsandboxed.
// The value is the pointer to a sandbox where the subcluster lives in
func (v *VerticaDB) findSclustersToUnsandbox(oldObj *VerticaDB) map[*Subcluster]*Sandbox {
	oldSubclusterInSandbox := oldObj.GenSubclusterSandboxMap()
	newSuclusterInSandbox := v.GenSubclusterSandboxMap()
	oldSubclusterMap := oldObj.GenSubclusterMap()
	oldSandboxMap := oldObj.GenSandboxMap()
	sclusterSboxMap := make(map[*Subcluster]*Sandbox)
	for oldSubclusterName, oldSandboxName := range oldSubclusterInSandbox {
		newSandboxName, oldSubclusterInNewSandboxes := newSuclusterInSandbox[oldSubclusterName]
		oldSandbox := oldSandboxMap[oldSandboxName]
		// for unsandboxing, check shutdown field of the subcluster and sandbox
		if !oldSubclusterInNewSandboxes || (oldSubclusterInNewSandboxes && oldSandbox.Name != newSandboxName) {
			// either subcluster is not in any sbox or is moved to a different sbox in new vdb
			oldSubcluster := oldSubclusterMap[oldSubclusterName]
			sclusterSboxMap[oldSubcluster] = oldSandbox
		}
	}
	return sclusterSboxMap
}

// checkUnsandboxShutdownConditions ensures we will not unsandbox a subcluster that has shutdown set to true, or a subcluster
// in a sandbox where the shutdown field for the sandbox is set to true
func (v *VerticaDB) checkUnsandboxShutdownConditions(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	statusSClusterIndexMap := v.GenStatusSClusterIndexMap()
	subclustersToUnsandbox := v.findSclustersToUnsandbox(oldObj)
	subclusterIndexMap := v.GenSubclusterIndexMap()
	sandboxIndexMap := v.GenSandboxIndexMap()
	sandboxesWithError := map[string]bool{}
	for oldSubcluster, oldSandbox := range subclustersToUnsandbox {
		oldSubclusterIndex, found := statusSClusterIndexMap[oldSubcluster.Name]
		if oldSubcluster.Shutdown || (found && v.Status.Subclusters[oldSubclusterIndex].Shutdown) {
			i := subclusterIndexMap[oldSubcluster.Name]
			p := field.NewPath("spec").Child("subclusters").Index(i)
			err := field.Invalid(p,
				oldSubcluster.Shutdown,
				fmt.Sprintf("cannot unsandbox subcluster %q that has Shutdown field set to true",
					oldSubcluster.Name))
			allErrs = append(allErrs, err)
			continue
		}
		if oldSandbox.Shutdown {
			i := sandboxIndexMap[oldSandbox.Name]
			_, found := sandboxesWithError[oldSandbox.Name]
			if !found { // this is to avoid duplicate error messages
				sandboxesWithError[oldSandbox.Name] = true
				p := field.NewPath("spec").Child("sandboxes").Index(i)
				err := field.Invalid(p,
					oldSubcluster.Name,
					fmt.Sprintf("cannot unsandbox subcluster %q in sandbox %q that has Shutdown field set to true",
						oldSubcluster.Name, oldSandbox.Name))
				allErrs = append(allErrs, err)
				continue
			}
		}
	}
	return allErrs
}

// checkSubclustersInShutdownSandbox ensures: when a sandbox is marked to be shutdown,
// the subcluster's shutdown field should not to set to false by a user
func (v *VerticaDB) checkSubclustersInShutdownSandbox(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	persistSandboxes := v.findPersistSandboxes(oldObj)
	newSandboxMap := v.GenSandboxMap()
	oldSandboxMap := oldObj.GenSandboxMap()
	newSubclusterIndexMap := v.GenSubclusterIndexMap()
	newSubclusterMap := v.GenSubclusterMap()
	oldSubclusterMap := oldObj.GenSubclusterMap()
	for sandboxName := range persistSandboxes {
		newSandbox := newSandboxMap[sandboxName]
		oldSandbox := oldSandboxMap[sandboxName]
		if newSandbox.Shutdown {
			for _, subclusterName := range oldSandbox.Subclusters {
				oldSubcluster := oldSubclusterMap[subclusterName.Name]
				newSubcluster, oldSclusterPersist := newSubclusterMap[subclusterName.Name]
				if oldSclusterPersist && oldSubcluster.Shutdown != newSubcluster.Shutdown && !newSubcluster.Shutdown {
					index := newSubclusterIndexMap[subclusterName.Name]
					p := field.NewPath("spec").Child("subclusters")
					err := field.Invalid(p.Index(index).Child("shutdown"),
						newSubcluster.Shutdown,
						fmt.Sprintf("cannot change Shutdown field for subcluster %q whose shutdown is driven by sandbox %q",
							subclusterName.Name, oldSandbox.Name))
					allErrs = append(allErrs, err)
				}
			}
		}
	}
	return allErrs
}

// checkShutdownSandboxImage ensures a sandbox's image is not updated after the sandbox or any of its subclusters are shut down
func (v *VerticaDB) checkShutdownSandboxImage(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	persistSandboxes := v.findPersistSandboxes(oldObj)
	oldSandboxMap := oldObj.GenSandboxMap()
	newSandboxMap := v.GenSandboxMap()
	newSandboxIndexMap := v.GenSandboxIndexMap()
	newSubclusterMap := v.GenSubclusterMap()
	statusSClusterIndexMap := v.GenStatusSClusterIndexMap()
	for sandboxName := range persistSandboxes {
		oldSandbox := oldSandboxMap[sandboxName]
		newSandbox := newSandboxMap[sandboxName]
		if oldSandbox.Image != newSandbox.Image {
			newSandboxIndex := newSandboxIndexMap[sandboxName]
			errMsgs := v.checkSboxForShutdown(newSandbox, newSubclusterMap, statusSClusterIndexMap)
			if len(errMsgs) != 0 {
				p := field.NewPath("spec").Child("sandboxes").Index(newSandboxIndex)
				err := field.Invalid(p.Child("image"),
					newSandbox.Image,
					fmt.Sprintf("cannot change the image for sandbox %q because %q",
						sandboxName, strings.Join(errMsgs, ",")))
				allErrs = append(allErrs, err)
			}
		}
	}
	return allErrs
}

// checkShutdownForSandboxesToBeRemoved ensures the sandbox to terminate is not shut down
func (v *VerticaDB) checkShutdownForSandboxesToBeRemoved(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	oldSandboxMap := oldObj.GenSandboxMap()
	newSandboxMap := v.GenSandboxMap()
	sandboxesToBeRemoved := vutil.MapKeyDiff(oldSandboxMap, newSandboxMap)
	for _, sandboxToBeRemoved := range sandboxesToBeRemoved {
		if oldSandboxMap[sandboxToBeRemoved].Shutdown {
			p := field.NewPath("spec").Child("sandboxes")
			err := field.Invalid(p,
				sandboxToBeRemoved,
				fmt.Sprintf("cannot remove sandbox %q that has Shutdown field set to true",
					sandboxToBeRemoved))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// checkShutdownForSubclustersToBeRemoved ensures the subclusters to be removed are not shut down
func (v *VerticaDB) checkShutdownForSubclustersToBeRemoved(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	oldSubclusterMap := oldObj.GenSubclusterMap()
	newSubclusterMap := v.GenSubclusterMap()
	statusSubclusterMap := v.GenStatusSubclusterMap()
	subclustersToBeRemoved := vutil.MapKeyDiff(oldSubclusterMap, newSubclusterMap)
	oldSubclusterIndexMap := oldObj.GenSubclusterIndexMap()
	p := field.NewPath("spec").Child("subclusters")
	for _, subclusterToBeRemoved := range subclustersToBeRemoved {
		subclusterstatus, foundInStatus := statusSubclusterMap[subclusterToBeRemoved]
		if oldSubclusterMap[subclusterToBeRemoved].Shutdown || (foundInStatus && subclusterstatus.Shutdown) {
			i := oldSubclusterIndexMap[subclusterToBeRemoved]
			err := field.Invalid(p.Index(i),
				subclusterToBeRemoved,
				fmt.Sprintf("cannot remove subcluster %q that has Shutdown field set to true",
					subclusterToBeRemoved))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// checkSboxForShutdown checks if sbox or its scluster has shutdown field set to true in spec/status
// It returns a slice of strings. Each string is an error message
func (v *VerticaDB) checkSboxForShutdown(newSandbox *Sandbox, newSClusterMap map[string]*Subcluster, statusSClusterIndexMap map[string]int) []string {
	errMsgs := []string{}
	if newSandbox.Shutdown {
		errMsgs = append(errMsgs, fmt.Sprintf("Shutdown field of sandbox %q is set to true", newSandbox.Name))
	}
	shutdownSubclusters := []string{}
	for _, subcluster := range newSandbox.Subclusters {
		i, foundInStatus := statusSClusterIndexMap[subcluster.Name]
		newSubcluster, foundSubcluster := newSClusterMap[subcluster.Name]
		if (foundSubcluster && newSubcluster.Shutdown) || (foundInStatus && v.Status.Subclusters[i].Shutdown) {
			shutdownSubclusters = append(shutdownSubclusters, subcluster.Name)
		}
	}
	if len(shutdownSubclusters) != 0 {
		errMsgs = append(errMsgs, fmt.Sprintf("Subclusters %q are marked for shut down or has already been shut down", strings.Join(shutdownSubclusters, ",")))
	}
	return errMsgs
}

// checkImmutableStsName ensures the statefulset name of the subcluster stays constant
func (v *VerticaDB) checkImmutableStsName(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// We have an annotation to control the sts name. We are not going to allow
	// this annotation to be added to existing subclusters. Otherwise, it will
	// regenerate a new statefulset and keep the old one around. The statefulset
	// name can only be set for new subclusters.
	oldScMap := oldObj.GenSubclusterMap()
	newScMap := v.GenSubclusterMap()
	scIndexMap := v.GenSubclusterIndexMap()
	for scName, oldSc := range oldScMap {
		// Find the subcluster in the new map.
		newSc, found := newScMap[scName]
		// Only concerned with changes of existing subclusters. So, we can skip if
		// its not found.
		if !found {
			continue
		}
		oldStsName := oldSc.GetStatefulSetName(oldObj)
		newStsName := newSc.GetStatefulSetName(v)
		if oldStsName != newStsName {
			scInx := scIndexMap[scName]
			path := field.NewPath("spec").Child("subclusters").Index(scInx).
				Child("annotations").Key(vmeta.StsNameOverrideAnnotation)
			err := field.Invalid(path,
				newStsName,
				fmt.Sprintf("Renaming the statefulset of subcluster %q after creation of the subcluster is not allowed",
					scName))
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// checkImmutableClientProxy will validate the proxy spec fields in vdb
func (v *VerticaDB) checkImmutableClientProxy(oldObj *VerticaDB, allErrs field.ErrorList) field.ErrorList {
	// annotation vertica.com/use-client-proxy cannot change after creation
	if v.Annotations[vmeta.UseVProxyAnnotation] != oldObj.Annotations[vmeta.UseVProxyAnnotation] {
		prefix := field.NewPath("metadata").Child("annotations")
		errMsg := fmt.Sprintf("annotation %s cannot change after creation", vmeta.UseVProxyAnnotation)
		err := field.Invalid(prefix.Key(vmeta.UseVProxyAnnotation),
			v.Annotations[vmeta.VClusterOpsAnnotation],
			errMsg)
		allErrs = append(allErrs, err)
	}
	// proxy.image cannot change after creation
	if v.Spec.Proxy != nil && oldObj.Spec.Proxy != nil && v.Spec.Proxy.Image != oldObj.Spec.Proxy.Image ||
		v.Spec.Proxy == nil && oldObj.Spec.Proxy != nil {
		err := field.Invalid(field.NewPath("spec").Child("proxy").Child("image"),
			v.Spec.Proxy.Image,
			"proxy.image cannot change after creation, otherwise, as doing so will disrupt current user connections")
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

// setDefaultSandboxImages will explicitly set the image in any sandbox
// that omitted it
func (v *VerticaDB) setDefaultSandboxImages() {
	for i := range v.Spec.Sandboxes {
		sb := &v.Spec.Sandboxes[i]
		if sb.Image == "" {
			sb.Image = v.Spec.Image
		}
	}
}

// setDefaultProxy will set default values for some proxy fields
func (v *VerticaDB) setDefaultProxy() {
	useProxy := vmeta.UseVProxy(v.Annotations)
	if useProxy {
		if v.Spec.Proxy == nil {
			v.Spec.Proxy = &Proxy{}
		}
		if v.Spec.Proxy.Image == "" {
			v.Spec.Proxy.Image = VProxyDefaultImage
		}
	} else if v.Spec.Proxy != nil {
		if *v.Spec.Proxy == (Proxy{}) {
			v.Spec.Proxy = nil
		}
	}
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		sc.setDefaultProxySubcluster(useProxy)
	}
}

func (s *Subcluster) setDefaultProxySubcluster(useProxy bool) {
	if useProxy {
		if s.Proxy == nil {
			s.Proxy = &ProxySubclusterConfig{}
		}
		// When proxy is enabled, if the user did not set
		// the resource or replica in the subcluster spec,
		// we will set them to their default values
		if s.Proxy.Resources == nil {
			s.Proxy.Resources = &v1.ResourceRequirements{}
		}
		if s.Proxy.Replicas == nil {
			rep := int32(VProxyDefaultReplicas)
			s.Proxy.Replicas = &rep
		}
		return
	}
	if s.Proxy == nil {
		return
	}
	if s.Proxy.Resources != nil {
		res := *s.Proxy.Resources
		if len(res.Limits) == 0 && len(res.Requests) == 0 {
			// This is needed because k8s will automatically initialize
			// resources to an empty struct. We explicitly set it to nil
			// so it does not appear in the spec if proxy is disabled
			s.Proxy.Resources = nil
		}
	}
}
