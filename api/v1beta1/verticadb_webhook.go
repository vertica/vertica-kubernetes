/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

//nolint:lll
package v1beta1

import (
	"fmt"
	"reflect"
	"strings"

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
	S3Prefix              = "s3://"
	GCloudPrefix          = "gs://"
	AzurePrefix           = "azb://"
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
	verticadblog.Info("default", "name", v.Name)

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
	verticadblog.Info("validate create", "name", v.Name)

	allErrs := v.validateVerticaDBSpec()
	if allErrs == nil {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaDBKind}, v.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaDB) ValidateUpdate(old runtime.Object) error {
	verticadblog.Info("validate update", "name", v.Name)

	allErrs := append(v.validateImmutableFields(old), v.validateVerticaDBSpec()...)
	if allErrs == nil {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaDBKind}, v.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaDB) ValidateDelete() error {
	verticadblog.Info("validate delete", "name", v.Name)

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
	// dataPath cannot change after creation
	if v.Spec.Local.DataPath != oldObj.Spec.Local.DataPath {
		err := field.Invalid(field.NewPath("spec").Child("local").Child("dataPath"),
			v.Spec.Local.DataPath,
			"dataPath cannot change after creation.")
		allErrs = append(allErrs, err)
	}
	// depotPath cannot change after creation
	if v.Spec.Local.DepotPath != oldObj.Spec.Local.DepotPath {
		err := field.Invalid(field.NewPath("spec").Child("local").Child("depotPath"),
			v.Spec.Local.DepotPath,
			"depotPath cannot change after creation.")
		allErrs = append(allErrs, err)
	}
	// shardCount cannot change after creation
	if v.Spec.ShardCount != oldObj.Spec.ShardCount {
		err := field.Invalid(field.NewPath("spec").Child("shardCount"),
			v.Spec.ShardCount,
			"shardCount cannot change after creation.")
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
	// local.requestSize cannot change after creation
	if v.Spec.Local.RequestSize.Cmp(oldObj.Spec.Local.RequestSize) != 0 {
		err := field.Invalid(field.NewPath("spec").Child("local").Child("requestSize"),
			v.Spec.Local.RequestSize,
			"local.requestSize cannot change after creation")
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
	return allErrs
}

func (v *VerticaDB) validateVerticaDBSpec() field.ErrorList {
	allErrs := v.hasAtLeastOneSC(field.ErrorList{})
	allErrs = v.hasValidInitPolicy(allErrs)
	allErrs = v.hasValidDBName(allErrs)
	allErrs = v.hasPrimarySubcluster(allErrs)
	allErrs = v.validateKsafety(allErrs)
	allErrs = v.validateCommunalPath(allErrs)
	allErrs = v.validateEndpoint(allErrs)
	allErrs = v.hasValidDomainName(allErrs)
	allErrs = v.credentialSecretExists(allErrs)
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
	case CommunalInitPolicyRevive:
	case CommunalInitPolicyScheduleOnly:
	default:
		err := field.Invalid(field.NewPath("spec").Child("initPolicy"),
			v.Spec.InitPolicy,
			"initPolicy should either be Create, Revive or ScheduleOnly.")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

func (v *VerticaDB) validateCommunalPath(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.InitPolicy == CommunalInitPolicyScheduleOnly {
		return allErrs
	}
	allPrefs := []string{S3Prefix, GCloudPrefix, AzurePrefix}
	allPrefs = append(allPrefs, hdfsPrefixes...)
	for _, pref := range allPrefs {
		if strings.HasPrefix(v.Spec.Communal.Path, pref) {
			return allErrs
		}
	}
	err := field.Invalid(field.NewPath("spec").Child("communal").Child("path"),
		v.Spec.Communal.Path,
		"communal.Path is not prefixed with an accepted type")
	return append(allErrs, err)
}

func (v *VerticaDB) validateEndpoint(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.InitPolicy == CommunalInitPolicyScheduleOnly {
		return allErrs
	}
	// Endpoint is ignored if communal path is HDFS or Azure
	if v.IsHDFS() || v.IsAzure() {
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

func (v *VerticaDB) credentialSecretExists(allErrs field.ErrorList) field.ErrorList {
	if v.Spec.InitPolicy == CommunalInitPolicyScheduleOnly {
		return allErrs
	}
	// Credential secrets are not needed if communal path is HDFS
	if v.IsHDFS() {
		return allErrs
	}
	// communal.credentialSecret must exist
	if v.Spec.Communal.CredentialSecret == "" {
		err := field.Invalid(field.NewPath("spec").Child("communal").Child("credentialSecret"),
			v.Spec.Communal.CredentialSecret,
			"communal.credentialSecret must exist")
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
		if !IsValidSubclusterName(sc.Name) {
			err := field.Invalid(field.NewPath("spec").Child("subcluster").Index(i).Child("name"),
				v.Spec.Subclusters[i],
				"is not a valid domain name")
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (v *VerticaDB) hasValidNodePort(allErrs field.ErrorList) field.ErrorList {
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		if sc.ServiceType == v1.ServiceTypeNodePort {
			if sc.NodePort != 0 && (sc.NodePort < portLowerBound || sc.NodePort > portUpperBound) {
				err := field.Invalid(field.NewPath("spec").Child("subclusters").Index(i).Child("nodePort"),
					v.Spec.Subclusters[i].NodePort,
					fmt.Sprintf(`nodePort must be 0 or in the range of %d-%d`, portLowerBound, portUpperBound))
				allErrs = append(allErrs, err)
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
			if sc1.Name == sc2.Name {
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
	invalidPaths = append(invalidPaths, v.Spec.Local.DataPath, v.Spec.Local.DepotPath)
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

func (v *VerticaDB) isImageChangeInProgress() bool {
	return len(v.Status.Conditions) > ImageChangeInProgressIndex &&
		v.Status.Conditions[ImageChangeInProgressIndex].Status == v1.ConditionTrue
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
