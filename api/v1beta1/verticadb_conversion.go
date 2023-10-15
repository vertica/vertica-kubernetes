/*
Copyright [2021-2023] Open Text.

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
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

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
		ImagePullPolicy:         src.ImagePullPolicy,
		ImagePullSecrets:        convertToLocalReferenceSlice(src.ImagePullSecrets),
		Image:                   src.Image,
		Labels:                  src.Labels,
		Annotations:             src.Annotations,
		AutoRestartVertica:      src.AutoRestartVertica,
		DBName:                  src.DBName,
		ShardCount:              src.ShardCount,
		SuperuserPasswordSecret: src.SuperuserPasswordSecret,
		LicenseSecret:           src.LicenseSecret,
		InitPolicy:              v1.CommunalInitPolicy(src.InitPolicy),
		UpgradePolicy:           v1.UpgradePolicyType(src.UpgradePolicy),
		ReviveOrder:             make([]v1.SubclusterPodCount, len(src.ReviveOrder)),
		Communal:                convertToCommunal(&src.Communal),
		HadoopConfig:            src.Communal.HadoopConfig,
		Local:                   convertToLocal(&src.Local),
		Subclusters:             make([]v1.Subcluster, len(src.Subclusters)),
		Sidecars:                src.Sidecars,
		Volumes:                 src.Volumes,
		VolumeMounts:            src.VolumeMounts,
		CertSecrets:             convertToLocalReferenceSlice(src.CertSecrets),
		KerberosSecret:          src.KerberosSecret,
		EncryptSpreadComm:       src.EncryptSpreadComm,
		SecurityContext:         src.SecurityContext,
		PodSecurityContext:      src.PodSecurityContext,
		HTTPServerTLSSecret:     src.HTTPServerTLSSecret,
		ReadinessProbeOverride:  src.ReadinessProbeOverride,
		LivenessProbeOverride:   src.LivenessProbeOverride,
		StartupProbeOverride:    src.StartupProbeOverride,
		ServiceAccountName:      src.ServiceAccountName,
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
		SuperuserPasswordSecret: srcSpec.SuperuserPasswordSecret,
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
		EncryptSpreadComm:       srcSpec.EncryptSpreadComm,
		SecurityContext:         srcSpec.SecurityContext,
		PodSecurityContext:      srcSpec.PodSecurityContext,
		HTTPServerTLSSecret:     srcSpec.HTTPServerTLSSecret,
		ReadinessProbeOverride:  srcSpec.ReadinessProbeOverride,
		LivenessProbeOverride:   srcSpec.LivenessProbeOverride,
		StartupProbeOverride:    srcSpec.StartupProbeOverride,
		ServiceAccountName:      srcSpec.ServiceAccountName,
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
		InstallCount:    src.InstallCount,
		AddedToDBCount:  src.AddedToDBCount,
		UpNodeCount:     src.UpNodeCount,
		SubclusterCount: src.SubclusterCount,
		Subclusters:     make([]v1.SubclusterStatus, len(src.Subclusters)),
		Conditions:      make([]v1.VerticaDBCondition, len(src.Conditions)),
		UpgradeStatus:   src.UpgradeStatus,
	}
	for i := range src.Subclusters {
		dst.Subclusters[i] = convertToSubclusterStatus(src.Subclusters[i])
	}
	for i := range src.Conditions {
		dst.Conditions[i] = convertToStatusCondition(src.Conditions[i])
	}
	return dst
}

// convertFromStatus will convert from a v1 VerticaDBStatus to a v1beta1 version
func convertFromStatus(src *v1.VerticaDBStatus) VerticaDBStatus {
	dst := VerticaDBStatus{
		InstallCount:    src.InstallCount,
		AddedToDBCount:  src.AddedToDBCount,
		UpNodeCount:     src.UpNodeCount,
		SubclusterCount: src.SubclusterCount,
		Subclusters:     make([]SubclusterStatus, len(src.Subclusters)),
		Conditions:      make([]VerticaDBCondition, len(src.Conditions)),
		UpgradeStatus:   src.UpgradeStatus,
	}
	for i := range src.Subclusters {
		dst.Subclusters[i] = convertFromSubclusterStatus(src.Subclusters[i])
	}
	for i := range src.Conditions {
		dst.Conditions[i] = convertFromStatusCondition(src.Conditions[i])
	}
	return dst
}

// convertToSubcluster will take a v1beta1 Subcluster and convert it to a v1 version
func convertToSubcluster(src *Subcluster) v1.Subcluster {
	return v1.Subcluster{
		Name:                src.Name,
		Size:                src.Size,
		IsPrimary:           src.IsPrimary,
		IsTransient:         src.IsTransient,
		ImageOverride:       src.ImageOverride,
		NodeSelector:        src.NodeSelector,
		Affinity:            v1.Affinity(src.Affinity),
		PriorityClassName:   src.PriorityClassName,
		Tolerations:         src.Tolerations,
		Resources:           src.Resources,
		ServiceType:         src.ServiceType,
		ServiceName:         src.ServiceName,
		NodePort:            src.NodePort,
		VerticaHTTPNodePort: src.VerticaHTTPNodePort,
		ExternalIPs:         src.ExternalIPs,
		LoadBalancerIP:      src.LoadBalancerIP,
		ServiceAnnotations:  src.ServiceAnnotations,
	}
}

// convertFromSubcluster will take a v1 Subcluster and convert it to a v1beta1 version
func convertFromSubcluster(src *v1.Subcluster) Subcluster {
	return Subcluster{
		Name:                src.Name,
		Size:                src.Size,
		IsPrimary:           src.IsPrimary,
		IsTransient:         src.IsTransient,
		ImageOverride:       src.ImageOverride,
		NodeSelector:        src.NodeSelector,
		Affinity:            Affinity(src.Affinity),
		PriorityClassName:   src.PriorityClassName,
		Tolerations:         src.Tolerations,
		Resources:           src.Resources,
		ServiceType:         src.ServiceType,
		ServiceName:         src.ServiceName,
		NodePort:            src.NodePort,
		VerticaHTTPNodePort: src.VerticaHTTPNodePort,
		ExternalIPs:         src.ExternalIPs,
		LoadBalancerIP:      src.LoadBalancerIP,
		ServiceAnnotations:  src.ServiceAnnotations,
	}
}

// convertToCommunal will convert to a v1 VerticaDBCondition from a v1beta1 version
func convertToCommunal(src *CommunalStorage) v1.CommunalStorage {
	return v1.CommunalStorage{
		Path:                   src.Path,
		Endpoint:               src.Endpoint,
		CredentialSecret:       src.CredentialSecret,
		CaFile:                 src.CaFile,
		Region:                 src.Region,
		KerberosServiceName:    src.KerberosServiceName,
		KerberosRealm:          src.KerberosRealm,
		S3ServerSideEncryption: v1.ServerSideEncryptionType(src.S3ServerSideEncryption),
		S3SseCustomerKeySecret: src.S3SseCustomerKeySecret,
		AdditionalConfig:       src.AdditionalConfig,
	}
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
		KerberosServiceName:    comSpec.KerberosServiceName,
		KerberosRealm:          comSpec.KerberosRealm,
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

// convetToSubcluterStatus will convert to a v1 SubcluterStatus from a v1beta1 version
func convertToSubclusterStatus(src SubclusterStatus) v1.SubclusterStatus {
	return v1.SubclusterStatus{
		Name:           src.Name,
		Oid:            src.Oid,
		InstallCount:   src.InstallCount,
		AddedToDBCount: src.AddedToDBCount,
		UpNodeCount:    src.UpNodeCount,
		ReadOnlyCount:  src.ReadOnlyCount,
		Detail:         convertToPodStatus(src.Detail),
	}
}

// convetFromSubcluterStatus will convert from a v1 SubcluterStatus to a v1beta1 version
func convertFromSubclusterStatus(src v1.SubclusterStatus) SubclusterStatus {
	return SubclusterStatus{
		Name:           src.Name,
		Oid:            src.Oid,
		InstallCount:   src.InstallCount,
		AddedToDBCount: src.AddedToDBCount,
		UpNodeCount:    src.UpNodeCount,
		ReadOnlyCount:  src.ReadOnlyCount,
		Detail:         convertFromPodStatus(src.Detail),
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
			ReadOnly:  src[i].ReadOnly,
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
			ReadOnly:  src[i].ReadOnly,
		}
	}
	return pods
}

// convertToStatusCondition will convert to a v1 VerticaDBCondition from a v1beta1 version
func convertToStatusCondition(src VerticaDBCondition) v1.VerticaDBCondition {
	return v1.VerticaDBCondition{
		Type:               v1.VerticaDBConditionType(src.Type),
		Status:             src.Status,
		LastTransitionTime: src.LastTransitionTime,
	}
}

// convertFromStatusCondition will convert from a v1 VerticaDBCondition to a v1beta1 version
func convertFromStatusCondition(src v1.VerticaDBCondition) VerticaDBCondition {
	return VerticaDBCondition{
		Type:               VerticaDBConditionType(src.Type),
		Status:             src.Status,
		LastTransitionTime: src.LastTransitionTime,
	}
}
