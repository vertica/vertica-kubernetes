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
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo is a function to convert a v1beta1 CR to the v1 version of the CR.
func (v *VerticaDB) ConvertTo(dstRaw conversion.Hub) error {
	verticadblog.Info("ConvertTo", "GroupVersion", GroupVersion, "name", v.Name, "namespace", v.Namespace, "uid", v.UID)
	dst := dstRaw.(*v1.VerticaDB)
	dst.Name = v.Name
	dst.Namespace = v.Namespace
	dst.Annotations = v.Annotations
	dst.UID = v.UID
	dst.Annotations = v.Annotations
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
	v.Annotations = src.Annotations
	v.UID = src.UID
	v.Annotations = src.Annotations
	v.Labels = src.Labels
	v.Spec = convertFromSpec(&src.Spec)
	v.Status = convertFromStatus(&src.Status)
	return nil
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
		IgnoreClusterLease:      src.IgnoreClusterLease,
		InitPolicy:              v1.CommunalInitPolicy(src.InitPolicy),
		UpgradePolicy:           v1.UpgradePolicyType(src.UpgradePolicy),
		IgnoreUpgradePath:       src.IgnoreUpgradePath,
		ReviveOrder:             make([]v1.SubclusterPodCount, len(src.ReviveOrder)),
		RestartTimeout:          src.RestartTimeout,
		Communal:                convertToCommunal(&src.Communal),
		HadoopConfig:            src.Communal.HadoopConfig,
		Local:                   convertToLocal(&src.Local),
		Subclusters:             make([]v1.Subcluster, len(src.Subclusters)),
		TemporarySubclusterRouting: v1.SubclusterSelection{
			Names:    src.TemporarySubclusterRouting.Names,
			Template: convertToSubcluster(&src.TemporarySubclusterRouting.Template),
		},
		KSafety:                  v1.KSafetyType(src.KSafety),
		RequeueTime:              src.RequeueTime,
		UpgradeRequeueTime:       src.UpgradeRequeueTime,
		Sidecars:                 src.Sidecars,
		Volumes:                  src.Volumes,
		VolumeMounts:             src.VolumeMounts,
		CertSecrets:              convertToLocalReferenceSlice(src.CertSecrets),
		KerberosSecret:           src.KerberosSecret,
		SSHSecret:                src.SSHSecret,
		EncryptSpreadComm:        src.EncryptSpreadComm,
		SecurityContext:          src.SecurityContext,
		PodSecurityContext:       src.PodSecurityContext,
		DeprecatedHTTPServerMode: v1.HTTPServerModeType(src.DeprecatedHTTPServerMode),
		HTTPServerTLSSecret:      src.HTTPServerTLSSecret,
		ReadinessProbeOverride:   src.ReadinessProbeOverride,
		LivenessProbeOverride:    src.LivenessProbeOverride,
		StartupProbeOverride:     src.StartupProbeOverride,
	}
	for i := range src.ReviveOrder {
		dst.ReviveOrder[i] = v1.SubclusterPodCount(src.ReviveOrder[i])
	}
	for i := range src.Subclusters {
		dst.Subclusters[i] = convertToSubcluster(&src.Subclusters[i])
	}
	return dst
}

// convertFromSpec will convert from a v1 VerticaDBSpec to a v1beta1 version
func convertFromSpec(src *v1.VerticaDBSpec) VerticaDBSpec {
	dst := VerticaDBSpec{
		ImagePullPolicy:         src.ImagePullPolicy,
		ImagePullSecrets:        convertFromLocalReferenceSlice(src.ImagePullSecrets),
		Image:                   src.Image,
		Labels:                  src.Labels,
		Annotations:             src.Annotations,
		AutoRestartVertica:      src.AutoRestartVertica,
		DBName:                  src.DBName,
		ShardCount:              src.ShardCount,
		SuperuserPasswordSecret: src.SuperuserPasswordSecret,
		LicenseSecret:           src.LicenseSecret,
		IgnoreClusterLease:      src.IgnoreClusterLease,
		InitPolicy:              CommunalInitPolicy(src.InitPolicy),
		UpgradePolicy:           UpgradePolicyType(src.UpgradePolicy),
		IgnoreUpgradePath:       src.IgnoreUpgradePath,
		ReviveOrder:             make([]SubclusterPodCount, len(src.ReviveOrder)),
		RestartTimeout:          src.RestartTimeout,
		Communal:                convertFromCommunal(&src.Communal, src.HadoopConfig),
		Local:                   convertFromLocal(&src.Local),
		Subclusters:             make([]Subcluster, len(src.Subclusters)),
		TemporarySubclusterRouting: SubclusterSelection{
			Names:    src.TemporarySubclusterRouting.Names,
			Template: convertFromSubcluster(&src.TemporarySubclusterRouting.Template),
		},
		KSafety:                  KSafetyType(src.KSafety),
		RequeueTime:              src.RequeueTime,
		UpgradeRequeueTime:       src.UpgradeRequeueTime,
		Sidecars:                 src.Sidecars,
		Volumes:                  src.Volumes,
		VolumeMounts:             src.VolumeMounts,
		CertSecrets:              convertFromLocalReferenceSlice(src.CertSecrets),
		KerberosSecret:           src.KerberosSecret,
		SSHSecret:                src.SSHSecret,
		EncryptSpreadComm:        src.EncryptSpreadComm,
		SecurityContext:          src.SecurityContext,
		PodSecurityContext:       src.PodSecurityContext,
		DeprecatedHTTPServerMode: HTTPServerModeType(src.DeprecatedHTTPServerMode),
		HTTPServerTLSSecret:      src.HTTPServerTLSSecret,
		ReadinessProbeOverride:   src.ReadinessProbeOverride,
		LivenessProbeOverride:    src.LivenessProbeOverride,
		StartupProbeOverride:     src.StartupProbeOverride,
	}
	for i := range src.ReviveOrder {
		dst.ReviveOrder[i] = SubclusterPodCount(src.ReviveOrder[i])
	}
	for i := range src.Subclusters {
		dst.Subclusters[i] = convertFromSubcluster(&src.Subclusters[i])
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
		IncludeUIDInPath:       src.IncludeUIDInPath,
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
func convertFromCommunal(src *v1.CommunalStorage, hadoopConfig string) CommunalStorage {
	return CommunalStorage{
		Path:                   src.Path,
		IncludeUIDInPath:       src.IncludeUIDInPath,
		Endpoint:               src.Endpoint,
		CredentialSecret:       src.CredentialSecret,
		HadoopConfig:           hadoopConfig,
		CaFile:                 src.CaFile,
		Region:                 src.Region,
		KerberosServiceName:    src.KerberosServiceName,
		KerberosRealm:          src.KerberosRealm,
		S3ServerSideEncryption: ServerSideEncryptionType(src.S3ServerSideEncryption),
		S3SseCustomerKeySecret: src.S3SseCustomerKeySecret,
		AdditionalConfig:       src.AdditionalConfig,
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
