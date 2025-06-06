/*
Copyright [2021-2024] Open Text.

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

package v1beta1

import (
	"strconv"

	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// log is for logging in this package.
var verticadblog = logf.Log.WithName("verticadb-resource")

// ConvertTo is a function to convert a v1beta1 CR to the v1 version of the CR.
func (v *VerticaDB) ConvertTo(dstRaw conversion.Hub) error {
	verticadblog.Info("ConvertTo", "GroupVersion", GroupVersion, "name", v.Name, "namespace", v.Namespace, "uid", v.UID)
	dst := dstRaw.(*v1.VerticaDB)
	dst.Name = v.Name
	dst.Namespace = v.Namespace
	dst.Annotations = convertToAnnotations(v)
	dst.UID = v.UID
	dst.Labels = v.Labels
	dst.Spec = convertToSpec(&v.Spec)
	dst.Status = convertToStatus(&v.Status)
	return nil
}

// ConvertFrom will handle conversion from the Hub type (v1) to v1beta1.
func (v *VerticaDB) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.VerticaDB)
	verticadblog.Info("ConvertFrom", "GroupVersion", GroupVersion, "name", src.Name, "namespace", src.Namespace, "uid", src.UID)
	v.Name = src.Name
	v.Namespace = src.Namespace
	v.Annotations = convertFromAnnotations(src)
	v.UID = src.UID
	v.Labels = src.Labels
	v.Spec = convertFromSpec(src)
	v.Status = convertFromStatus(&src.Status)
	return nil
}

// convertToAnnotations will create the annotations for a v1.VerticaDB CR taken
// from a v1beta1.VerticaDB.
func convertToAnnotations(src *VerticaDB) (newAnnotations map[string]string) {
	newAnnotations = make(map[string]string, len(src.Annotations))
	for k, v := range src.Annotations {
		newAnnotations[k] = v
	}
	// Each parameter in the v1 API that we removed will have a corresponding
	// annotation to allow conversion between the two versions. But we only want
	// to set the annotation if the parameter values wasn't the default.
	if src.Spec.IgnoreClusterLease {
		newAnnotations[vmeta.IgnoreClusterLeaseAnnotation] = strconv.FormatBool(src.Spec.IgnoreClusterLease)
	}
	if src.Spec.IgnoreUpgradePath {
		newAnnotations[vmeta.IgnoreUpgradePathAnnotation] = strconv.FormatBool(src.Spec.IgnoreUpgradePath)
	}
	if src.Spec.RestartTimeout != 0 {
		newAnnotations[vmeta.RestartTimeoutAnnotation] = strconv.FormatInt(int64(src.Spec.RestartTimeout), 10)
	}
	if src.Spec.KSafety != KSafetyType(vmeta.KSafetyDefaultValue) {
		newAnnotations[vmeta.KSafetyAnnotation] = string(src.Spec.KSafety)
	}
	if src.Spec.RequeueTime != 0 {
		newAnnotations[vmeta.RequeueTimeAnnotation] = strconv.FormatInt(int64(src.Spec.RequeueTime), 10)
	}
	if src.Spec.UpgradeRequeueTime != 0 {
		newAnnotations[vmeta.UpgradeRequeueTimeAnnotation] = strconv.FormatInt(int64(src.Spec.UpgradeRequeueTime), 10)
	}
	if src.Spec.SSHSecret != "" {
		newAnnotations[vmeta.SSHSecAnnotation] = src.Spec.SSHSecret
	}
	if src.Spec.Communal.IncludeUIDInPath {
		newAnnotations[vmeta.IncludeUIDInPathAnnotation] = strconv.FormatBool(src.Spec.Communal.IncludeUIDInPath)
	}
	// always relax k-safety check for v1beta1
	newAnnotations[vmeta.StrictKSafetyCheckAnnotation] = strconv.FormatBool(false)
	_, VClusterOpsAnnotationOK := src.Annotations[vmeta.VClusterOpsAnnotation]
	// If the VClusterOpsAnnotation annotation is not there, add it so that CRs converted
	// from v1beta1 will still use admintools deployments. v1 APIs already have
	// a default deployment of vclusterops.
	if !VClusterOpsAnnotationOK {
		newAnnotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
	}
	return newAnnotations
}

// convertFromAnnotations will create the annotations for a v1beta1.VerticaDB CR taken
// from a v1.VerticaDB.
func convertFromAnnotations(src *v1.VerticaDB) (newAnnotations map[string]string) {
	newAnnotations = make(map[string]string, len(src.Annotations))
	// Some annotations, which are used to fill CR parameters are left off
	// during the conversion. We don't want two sources for the same info.
	omitKeys := map[string]any{
		vmeta.IgnoreClusterLeaseAnnotation: true,
		vmeta.IgnoreUpgradePathAnnotation:  true,
		vmeta.RestartTimeoutAnnotation:     true,
		vmeta.KSafetyAnnotation:            true,
		vmeta.RequeueTimeAnnotation:        true,
		vmeta.UpgradeRequeueTimeAnnotation: true,
		vmeta.SSHSecAnnotation:             true,
		vmeta.IncludeUIDInPathAnnotation:   true,
	}
	for key, val := range src.Annotations {
		if _, ok := omitKeys[key]; ok {
			continue
		}
		newAnnotations[key] = val
	}
	return
}

// convertToSpec will convert to a v1 VerticaDBSpec from a v1beta1 version
func convertToSpec(src *VerticaDBSpec) v1.VerticaDBSpec {
	dst := v1.VerticaDBSpec{
		ImagePullPolicy:        src.ImagePullPolicy,
		ImagePullSecrets:       convertToLocalReferenceSlice(src.ImagePullSecrets),
		Image:                  src.Image,
		Labels:                 src.Labels,
		Annotations:            src.Annotations,
		AutoRestartVertica:     src.AutoRestartVertica,
		DBName:                 src.DBName,
		ShardCount:             src.ShardCount,
		PasswordSecret:         src.SuperuserPasswordSecret,
		LicenseSecret:          src.LicenseSecret,
		InitPolicy:             v1.CommunalInitPolicy(src.InitPolicy),
		UpgradePolicy:          v1.UpgradePolicyType(src.UpgradePolicy),
		ReviveOrder:            make([]v1.SubclusterPodCount, len(src.ReviveOrder)),
		Communal:               convertToCommunal(&src.Communal),
		HadoopConfig:           src.Communal.HadoopConfig,
		Local:                  convertToLocal(&src.Local),
		Subclusters:            make([]v1.Subcluster, len(src.Subclusters)),
		Sidecars:               src.Sidecars,
		Volumes:                src.Volumes,
		VolumeMounts:           src.VolumeMounts,
		CertSecrets:            convertToLocalReferenceSlice(src.CertSecrets),
		KerberosSecret:         src.KerberosSecret,
		EncryptSpreadComm:      convertToEncryptSpreadComm(src.EncryptSpreadComm),
		SecurityContext:        src.SecurityContext,
		NMASecurityContext:     src.NMASecurityContext,
		PodSecurityContext:     src.PodSecurityContext,
		HTTPSNMATLSSecret:      src.HTTPServerTLSSecret,
		ClientServerTLSSecret:  src.ClientServerTLSSecret,
		ClientServerTLSMode:    src.ClientServerTLSMode,
		ReadinessProbeOverride: src.ReadinessProbeOverride,
		LivenessProbeOverride:  src.LivenessProbeOverride,
		StartupProbeOverride:   src.StartupProbeOverride,
		ServiceAccountName:     src.ServiceAccountName,
		ServiceHTTPSPort:       src.ServiceHTTPSPort,
		ServiceClientPort:      src.ServiceClientPort,
		Sandboxes:              convertToSandboxSlice(src.Sandboxes),
	}
	if src.Proxy != nil {
		dst.Proxy = &v1.Proxy{
			Image:     src.Proxy.Image,
			TLSSecret: src.Proxy.TLSSecret,
		}
	}
	if src.RestorePoint != nil {
		dst.RestorePoint = &v1.RestorePointPolicy{
			Archive:          src.RestorePoint.Archive,
			Index:            src.RestorePoint.Index,
			ID:               src.RestorePoint.ID,
			NumRestorePoints: src.RestorePoint.NumRestorePoints,
		}
	}
	for i := range src.ReviveOrder {
		dst.ReviveOrder[i] = v1.SubclusterPodCount(src.ReviveOrder[i])
	}
	for i := range src.Subclusters {
		dst.Subclusters[i] = convertToSubcluster(&src.Subclusters[i])
	}
	if src.RequiresTransientSubcluster() || len(src.TemporarySubclusterRouting.Names) > 0 {
		dst.TemporarySubclusterRouting = &v1.SubclusterSelection{
			Names:    src.TemporarySubclusterRouting.Names,
			Template: convertToSubcluster(&src.TemporarySubclusterRouting.Template),
		}
	}
	return dst
}

// convertFromSpec will convert from a v1 VerticaDBSpec to a v1beta1 version
func convertFromSpec(src *v1.VerticaDB) VerticaDBSpec {
	srcSpec := &src.Spec
	dst := VerticaDBSpec{
		ImagePullPolicy:         srcSpec.ImagePullPolicy,
		ImagePullSecrets:        convertFromLocalReferenceSlice(srcSpec.ImagePullSecrets),
		Image:                   srcSpec.Image,
		Labels:                  srcSpec.Labels,
		Annotations:             srcSpec.Annotations,
		AutoRestartVertica:      srcSpec.AutoRestartVertica,
		DBName:                  srcSpec.DBName,
		ShardCount:              srcSpec.ShardCount,
		SuperuserPasswordSecret: srcSpec.PasswordSecret,
		LicenseSecret:           srcSpec.LicenseSecret,
		IgnoreClusterLease:      src.GetIgnoreClusterLease(),
		InitPolicy:              CommunalInitPolicy(srcSpec.InitPolicy),
		UpgradePolicy:           UpgradePolicyType(srcSpec.UpgradePolicy),
		IgnoreUpgradePath:       src.GetIgnoreUpgradePath(),
		ReviveOrder:             make([]SubclusterPodCount, len(srcSpec.ReviveOrder)),
		RestartTimeout:          src.GetRestartTimeout(),
		Communal:                convertFromCommunal(src),
		Local:                   convertFromLocal(&srcSpec.Local),
		Subclusters:             make([]Subcluster, len(srcSpec.Subclusters)),
		KSafety:                 KSafetyType(src.GetKSafety()),
		RequeueTime:             src.GetRequeueTime(),
		UpgradeRequeueTime:      src.GetUpgradeRequeueTime(),
		Sidecars:                srcSpec.Sidecars,
		Volumes:                 srcSpec.Volumes,
		VolumeMounts:            srcSpec.VolumeMounts,
		CertSecrets:             convertFromLocalReferenceSlice(srcSpec.CertSecrets),
		KerberosSecret:          srcSpec.KerberosSecret,
		SSHSecret:               src.GetSSHSecretName(),
		EncryptSpreadComm:       convertFromEncryptSpreadComm(srcSpec.EncryptSpreadComm),
		SecurityContext:         srcSpec.SecurityContext,
		NMASecurityContext:      srcSpec.NMASecurityContext,
		PodSecurityContext:      srcSpec.PodSecurityContext,
		HTTPServerTLSSecret:     srcSpec.HTTPSNMATLSSecret,
		ClientServerTLSSecret:   srcSpec.ClientServerTLSSecret,
		ClientServerTLSMode:     srcSpec.ClientServerTLSMode,
		ReadinessProbeOverride:  srcSpec.ReadinessProbeOverride,
		LivenessProbeOverride:   srcSpec.LivenessProbeOverride,
		StartupProbeOverride:    srcSpec.StartupProbeOverride,
		ServiceAccountName:      srcSpec.ServiceAccountName,
		ServiceHTTPSPort:        srcSpec.ServiceHTTPSPort,
		ServiceClientPort:       srcSpec.ServiceClientPort,
		Sandboxes:               convertFromSandboxSlice(srcSpec.Sandboxes),
	}
	if srcSpec.Proxy != nil {
		dst.Proxy = &Proxy{
			Image:     srcSpec.Proxy.Image,
			TLSSecret: srcSpec.Proxy.TLSSecret,
		}
	}
	if srcSpec.RestorePoint != nil {
		dst.RestorePoint = &RestorePointPolicy{
			Archive:          srcSpec.RestorePoint.Archive,
			Index:            srcSpec.RestorePoint.Index,
			ID:               srcSpec.RestorePoint.ID,
			NumRestorePoints: srcSpec.RestorePoint.NumRestorePoints,
		}
	}
	for i := range srcSpec.ReviveOrder {
		dst.ReviveOrder[i] = SubclusterPodCount(srcSpec.ReviveOrder[i])
	}
	for i := range srcSpec.Subclusters {
		dst.Subclusters[i] = convertFromSubcluster(&srcSpec.Subclusters[i])
	}
	if srcSpec.TemporarySubclusterRouting != nil {
		dst.TemporarySubclusterRouting = SubclusterSelection{
			Template: convertFromSubcluster(&srcSpec.TemporarySubclusterRouting.Template),
			Names:    srcSpec.TemporarySubclusterRouting.Names,
		}
	}
	return dst
}

// convertToStatus will convert to a v1 VerticaDBStatus from a v1beta1 version
func convertToStatus(src *VerticaDBStatus) v1.VerticaDBStatus {
	dst := v1.VerticaDBStatus{
		AddedToDBCount:  src.AddedToDBCount,
		UpNodeCount:     src.UpNodeCount,
		SubclusterCount: src.SubclusterCount,
		Subclusters:     make([]v1.SubclusterStatus, len(src.Subclusters)),
		Conditions:      make([]metav1.Condition, 0),
		UpgradeStatus:   src.UpgradeStatus,
		Sandboxes:       make([]v1.SandboxStatus, len(src.Sandboxes)),
		SecretRefs:      make([]v1.SecretRef, len(src.SecretRefs)),
		TLSModes:        make([]v1.TLSMode, len(src.TLSModes)),
	}
	if src.RestorePoint != nil {
		dst.RestorePoint = &v1.RestorePointInfo{
			Archive:        src.RestorePoint.Archive,
			StartTimestamp: src.RestorePoint.StartTimestamp,
			EndTimestamp:   src.RestorePoint.EndTimestamp,
		}
	}
	for i := range src.Subclusters {
		dst.Subclusters[i] = convertToSubclusterStatus(&src.Subclusters[i])
	}
	for i := range src.Conditions {
		meta.SetStatusCondition(&dst.Conditions, convertToStatusCondition(src.Conditions[i]))
	}
	for i := range src.Sandboxes {
		dst.Sandboxes[i] = convertToSandboxStatus(src.Sandboxes[i])
	}
	for i := range src.SecretRefs {
		dst.SecretRefs[i] = v1.SecretRef{
			Name: src.SecretRefs[i].Name,
			Type: src.SecretRefs[i].Type,
		}
	}
	for i := range src.TLSModes {
		dst.TLSModes[i] = v1.TLSMode{
			Mode: src.TLSModes[i].Mode,
			Type: src.TLSModes[i].Type,
		}
	}
	return dst
}

// convertFromStatus will convert from a v1 VerticaDBStatus to a v1beta1 version
func convertFromStatus(src *v1.VerticaDBStatus) VerticaDBStatus {
	dst := VerticaDBStatus{
		InstallCount:    src.InstallCount(),
		AddedToDBCount:  src.AddedToDBCount,
		UpNodeCount:     src.UpNodeCount,
		SubclusterCount: src.SubclusterCount,
		Subclusters:     make([]SubclusterStatus, len(src.Subclusters)),
		Conditions:      make([]VerticaDBCondition, len(src.Conditions)),
		UpgradeStatus:   src.UpgradeStatus,
		Sandboxes:       make([]SandboxStatus, len(src.Sandboxes)),
		SecretRefs:      make([]SecretRef, len(src.SecretRefs)),
		TLSModes:        make([]TLSMode, len(src.TLSModes)),
	}
	if src.RestorePoint != nil {
		dst.RestorePoint = &RestorePointInfo{
			Archive:        src.RestorePoint.Archive,
			StartTimestamp: src.RestorePoint.StartTimestamp,
			EndTimestamp:   src.RestorePoint.EndTimestamp,
		}
	}
	for i := range src.Subclusters {
		dst.Subclusters[i] = convertFromSubclusterStatus(&src.Subclusters[i])
	}
	for i := range src.Conditions {
		dst.Conditions[i] = convertFromStatusCondition(&src.Conditions[i])
	}
	for i := range src.Sandboxes {
		dst.Sandboxes[i] = convertFromSandboxStatus(src.Sandboxes[i])
	}
	for i := range src.SecretRefs {
		dst.SecretRefs[i] = SecretRef{
			Name: src.SecretRefs[i].Name,
			Type: src.SecretRefs[i].Type,
		}
	}
	for i := range src.TLSModes {
		dst.TLSModes[i] = TLSMode{
			Mode: src.TLSModes[i].Mode,
			Type: src.TLSModes[i].Type,
		}
	}
	return dst
}

// convertToSubcluster will take a v1beta1 Subcluster and convert it to a v1 version
func convertToSubcluster(src *Subcluster) v1.Subcluster {
	dst := v1.Subcluster{
		Name:                src.Name,
		Size:                src.Size,
		Type:                convertToSubclusterType(src),
		ImageOverride:       src.ImageOverride,
		NodeSelector:        src.NodeSelector,
		Affinity:            v1.Affinity(src.Affinity),
		PriorityClassName:   src.PriorityClassName,
		Tolerations:         src.Tolerations,
		Resources:           src.Resources,
		ServiceType:         src.ServiceType,
		ServiceName:         src.ServiceName,
		ClientNodePort:      src.NodePort,
		VerticaHTTPNodePort: src.VerticaHTTPNodePort,
		ServiceHTTPSPort:    src.ServiceHTTPSPort,
		ServiceClientPort:   src.ServiceClientPort,
		ExternalIPs:         src.ExternalIPs,
		LoadBalancerIP:      src.LoadBalancerIP,
		ServiceAnnotations:  src.ServiceAnnotations,
		Annotations:         src.Annotations,
	}
	if src.Proxy != nil {
		dst.Proxy = &v1.ProxySubclusterConfig{
			Replicas:  ptrOrNil(src.Proxy.Replicas),
			Resources: ptrOrNil(src.Proxy.Resources),
		}
	}
	return dst
}

// convertFromSubcluster will take a v1 Subcluster and convert it to a v1beta1 version
func convertFromSubcluster(src *v1.Subcluster) Subcluster {
	dst := Subcluster{
		Name:                src.Name,
		Size:                src.Size,
		IsPrimary:           src.IsPrimary(),
		IsTransient:         src.IsTransient(),
		IsSandboxPrimary:    src.IsSandboxPrimary(),
		ImageOverride:       src.ImageOverride,
		NodeSelector:        src.NodeSelector,
		Affinity:            Affinity(src.Affinity),
		PriorityClassName:   src.PriorityClassName,
		Tolerations:         src.Tolerations,
		Resources:           src.Resources,
		ServiceType:         src.ServiceType,
		ServiceName:         src.ServiceName,
		NodePort:            src.ClientNodePort,
		VerticaHTTPNodePort: src.VerticaHTTPNodePort,
		ServiceHTTPSPort:    src.ServiceHTTPSPort,
		ServiceClientPort:   src.ServiceClientPort,
		ExternalIPs:         src.ExternalIPs,
		LoadBalancerIP:      src.LoadBalancerIP,
		ServiceAnnotations:  src.ServiceAnnotations,
		Annotations:         src.Annotations,
	}
	if src.Proxy != nil {
		dst.Proxy = &ProxySubclusterConfig{
			Replicas:  ptrOrNil(src.Proxy.Replicas),
			Resources: ptrOrNil(src.Proxy.Resources),
		}
	}
	return dst
}

// convertToCommunal will convert to a v1 CommunalStorage from a v1beta1 version
func convertToCommunal(src *CommunalStorage) v1.CommunalStorage {
	cs := v1.CommunalStorage{
		Path:                   src.Path,
		Endpoint:               src.Endpoint,
		CredentialSecret:       src.CredentialSecret,
		CaFile:                 src.CaFile,
		Region:                 src.Region,
		S3ServerSideEncryption: v1.ServerSideEncryptionType(src.S3ServerSideEncryption),
		S3SseCustomerKeySecret: src.S3SseCustomerKeySecret,
		AdditionalConfig:       src.AdditionalConfig,
	}
	if src.KerberosServiceName != "" {
		cs.AdditionalConfig[vmeta.KerberosServiceNameConfig] = src.KerberosServiceName
	}
	if src.KerberosRealm != "" {
		cs.AdditionalConfig[vmeta.KerberosRealmConfig] = src.KerberosRealm
	}
	return cs
}

// convertFromCommunal will convert from a v1 CommunalStorage to a v1beta1 version
func convertFromCommunal(src *v1.VerticaDB) CommunalStorage {
	comSpec := src.Spec.Communal
	return CommunalStorage{
		Path:                   comSpec.Path,
		IncludeUIDInPath:       src.IncludeUIDInPath(),
		Endpoint:               comSpec.Endpoint,
		CredentialSecret:       comSpec.CredentialSecret,
		HadoopConfig:           src.Spec.HadoopConfig,
		CaFile:                 comSpec.CaFile,
		Region:                 comSpec.Region,
		KerberosServiceName:    comSpec.AdditionalConfig[vmeta.KerberosServiceNameConfig],
		KerberosRealm:          comSpec.AdditionalConfig[vmeta.KerberosRealmConfig],
		S3ServerSideEncryption: ServerSideEncryptionType(comSpec.S3ServerSideEncryption),
		S3SseCustomerKeySecret: comSpec.S3SseCustomerKeySecret,
		AdditionalConfig:       comSpec.AdditionalConfig,
	}
}

// convertToLocal will convert LocalStorage between v1beta1 to v1 versions
func convertToLocal(src *LocalStorage) v1.LocalStorage {
	return v1.LocalStorage{
		StorageClass: src.StorageClass,
		RequestSize:  src.RequestSize,
		DataPath:     src.DataPath,
		DepotPath:    src.DepotPath,
		DepotVolume:  v1.DepotVolumeType(src.DepotVolume),
		CatalogPath:  src.CatalogPath,
	}
}

// convertFromLocal will convert LocalStorage between v1 to v1beta1 versions
func convertFromLocal(src *v1.LocalStorage) LocalStorage {
	return LocalStorage{
		StorageClass: src.StorageClass,
		RequestSize:  src.RequestSize,
		DataPath:     src.DataPath,
		DepotPath:    src.DepotPath,
		DepotVolume:  DepotVolumeType(src.DepotVolume),
		CatalogPath:  src.CatalogPath,
	}
}

// convertToLocalReferenceSlice will convert a []LocalObjectReference from v1beta1
// to v1 versions
func convertToLocalReferenceSlice(src []LocalObjectReference) []v1.LocalObjectReference {
	dst := make([]v1.LocalObjectReference, len(src))
	for i := range src {
		dst[i] = v1.LocalObjectReference(src[i])
	}
	return dst
}

// convertFromLocalReferenceSlice will convert a []LocalObjectReference from v1
// to v1beta1 versions
func convertFromLocalReferenceSlice(src []v1.LocalObjectReference) []LocalObjectReference {
	dst := make([]LocalObjectReference, len(src))
	for i := range src {
		dst[i] = LocalObjectReference(src[i])
	}
	return dst
}

// convertToSandboxSlice will convert a []Sandbox from v1beta1
// to v1 versions
func convertToSandboxSlice(src []Sandbox) []v1.Sandbox {
	dst := make([]v1.Sandbox, len(src))
	for i := range src {
		dst[i] = v1.Sandbox{
			Name:        src[i].Name,
			Image:       src[i].Image,
			Subclusters: convertToSandboxSubclusterSlice(src[i].Subclusters),
		}
	}
	return dst
}

// convertFromSandboxSlice will convert a []Sandbox from v1
// to v1beta1 versions
func convertFromSandboxSlice(src []v1.Sandbox) []Sandbox {
	dst := make([]Sandbox, len(src))
	for i := range src {
		dst[i] = Sandbox{
			Name:        src[i].Name,
			Image:       src[i].Image,
			Subclusters: convertFromSandboxSubclusterSlice(src[i].Subclusters),
		}
	}
	return dst
}

// convertToSandboxSubclusterSlice will convert a []SandboxSubcluster from v1beta1
// to v1 versions
func convertToSandboxSubclusterSlice(src []SandboxSubcluster) []v1.SandboxSubcluster {
	dst := make([]v1.SandboxSubcluster, len(src))
	for i := range src {
		dst[i] = v1.SandboxSubcluster(src[i])
	}
	return dst
}

// convertFromSandboxSubclusterSlice will convert a []SandboxSubcluster from v1
// to v1beta1 versions
func convertFromSandboxSubclusterSlice(src []v1.SandboxSubcluster) []SandboxSubcluster {
	dst := make([]SandboxSubcluster, len(src))
	for i := range src {
		dst[i] = SandboxSubcluster(src[i])
	}
	return dst
}

// convetToSubcluterStatus will convert to a v1 SubcluterStatus from a v1beta1 version
func convertToSubclusterStatus(src *SubclusterStatus) v1.SubclusterStatus {
	return v1.SubclusterStatus{
		Name:           src.Name,
		Oid:            src.Oid,
		Type:           src.Type,
		AddedToDBCount: src.AddedToDBCount,
		UpNodeCount:    src.UpNodeCount,
		Detail:         convertToPodStatus(src.Detail),
	}
}

// convetFromSubcluterStatus will convert from a v1 SubcluterStatus to a v1beta1 version
func convertFromSubclusterStatus(src *v1.SubclusterStatus) SubclusterStatus {
	return SubclusterStatus{
		Name:           src.Name,
		Oid:            src.Oid,
		InstallCount:   src.InstallCount(),
		AddedToDBCount: src.AddedToDBCount,
		UpNodeCount:    src.UpNodeCount,
		Detail:         convertFromPodStatus(src.Detail),
	}
}

// convertToSandboxStatus will convert to a v1 SubcluterStatus from a v1beta1 version
func convertToSandboxStatus(src SandboxStatus) v1.SandboxStatus {
	return v1.SandboxStatus{
		Name:         src.Name,
		Subclusters:  src.Subclusters,
		UpgradeState: v1.SandboxUpgradeState(src.UpgradeState),
	}
}

// convertFromSandboxStatus will convert from a v1 SubcluterStatus to a v1beta1 version
func convertFromSandboxStatus(src v1.SandboxStatus) SandboxStatus {
	return SandboxStatus{
		Name:         src.Name,
		Subclusters:  src.Subclusters,
		UpgradeState: SandboxUpgradeState(src.UpgradeState),
	}
}

// convertToPodStatus will convert to a v1 VerticaDBPodStatus from a v1beta1 version
func convertToPodStatus(src []VerticaDBPodStatus) []v1.VerticaDBPodStatus {
	pods := make([]v1.VerticaDBPodStatus, len(src))
	for i := range src {
		pods[i] = v1.VerticaDBPodStatus{
			Installed: src[i].Installed,
			AddedToDB: src[i].AddedToDB,
			VNodeName: src[i].VNodeName,
			UpNode:    src[i].UpNode,
		}
	}
	return pods
}

// convertFromPodStatus will convert from a v1 VerticaDBPodStatus to a v1beta1 version
func convertFromPodStatus(src []v1.VerticaDBPodStatus) []VerticaDBPodStatus {
	pods := make([]VerticaDBPodStatus, len(src))
	for i := range src {
		pods[i] = VerticaDBPodStatus{
			Installed: src[i].Installed,
			AddedToDB: src[i].AddedToDB,
			VNodeName: src[i].VNodeName,
			UpNode:    src[i].UpNode,
		}
	}
	return pods
}

// convertToStatusCondition will convert to a v1 metav1.Condition from a v1beta1 VerticaDBCondition
func convertToStatusCondition(src VerticaDBCondition) metav1.Condition {
	cond := v1.MakeCondition(convertToStatusConditionType(src.Type),
		metav1.ConditionStatus(src.Status), "")
	cond.LastTransitionTime = src.LastTransitionTime
	return *cond
}

// convertFromStatusCondition will convert from a v1 metav1.Condition to a v1beta1 VerticaDBCondition
func convertFromStatusCondition(src *metav1.Condition) VerticaDBCondition {
	return VerticaDBCondition{
		Type:               convertFromStatusConditionType(src.Type),
		Status:             corev1.ConditionStatus(src.Status),
		LastTransitionTime: src.LastTransitionTime,
	}
}

// convertToSubclusterType returns the v1 Subcluster type for a given v1beta1
// Subcluster
func convertToSubclusterType(src *Subcluster) string {
	if src.IsSandboxPrimary {
		return v1.SandboxPrimarySubcluster
	}
	if src.IsPrimary {
		return v1.PrimarySubcluster
	}
	if src.IsTransient {
		return v1.TransientSubcluster
	}
	return v1.SecondarySubcluster
}

// convertToStatusConditionType will convert to a v1 ConditionType str from a v1beta1 VerticaDBConditionType
func convertToStatusConditionType(srcType VerticaDBConditionType) string {
	if srcType == ImageChangeInProgress {
		return v1.UpgradeInProgress
	}
	return string(srcType)
}

// convertFromStatusConditionType will convert from a v1 ConditionType str to a v1beta1 VerticaDBConditionType
func convertFromStatusConditionType(srcType string) VerticaDBConditionType {
	if srcType == v1.UpgradeInProgress {
		return ImageChangeInProgress
	}
	return VerticaDBConditionType(srcType)
}

// convertToEncryptSpreadComm will convert a v1beta1 EncryptSpreadComm to a v1 EncryptSpreadComm
func convertToEncryptSpreadComm(srcType string) string {
	// empty string in v1beta1 means disabling spread communication encryption
	if srcType == "" {
		return v1.EncryptSpreadCommDisabled
	}
	return srcType
}

// convertFromEncryptSpreadComm will convert a v1 EncryptSpreadComm to a v1beta1 EncryptSpreadComm
func convertFromEncryptSpreadComm(srcType string) string {
	if srcType == v1.EncryptSpreadCommDisabled {
		return ""
	}
	// except for "disabled", other values(empty string and "vertica") in v1 mean
	// enabling spread communication encryption with vertica key
	return EncryptSpreadCommWithVertica
}
