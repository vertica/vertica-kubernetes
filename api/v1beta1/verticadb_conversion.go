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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// SPILLY - address funlen linting errors in here

// ConvertTo is a function to convert a v1beta1 CR to the v1 version of the CR.
//
//nolint:funlen
func (v *VerticaDB) ConvertTo(dstRaw conversion.Hub) error {
	verticadblog.Info("ConvertTo", "name", v.Name, "namespace", v.Namespace, "uid", v.UID)
	dst := dstRaw.(*v1.VerticaDB)
	dst.Name = v.Name
	dst.Namespace = v.Namespace
	dst.Annotations = v.Annotations
	dst.UID = v.UID
	dst.Annotations = addConversionAnnotations(dst.Annotations)
	dst.Labels = v.Labels
	dst.Spec.ImagePullPolicy = v.Spec.ImagePullPolicy
	dst.Spec.ImagePullSecrets = convertFromLocalReferenceSlice(v.Spec.ImagePullSecrets)
	dst.Spec.Image = v.Spec.Image
	dst.Spec.Labels = v.Spec.Labels
	dst.Spec.Annotations = v.Spec.Annotations
	dst.Spec.AutoRestartVertica = v.Spec.AutoRestartVertica
	dst.Spec.DBName = v.Spec.DBName
	dst.Spec.ShardCount = v.Spec.ShardCount
	dst.Spec.SuperuserPasswordSecret = v.Spec.SuperuserPasswordSecret
	dst.Spec.LicenseSecret = v.Spec.LicenseSecret
	dst.Spec.IgnoreClusterLease = v.Spec.IgnoreClusterLease
	dst.Spec.InitPolicy = v1.CommunalInitPolicy(v.Spec.InitPolicy)
	dst.Spec.UpgradePolicy = v1.UpgradePolicyType(v.Spec.UpgradePolicy)
	dst.Spec.IgnoreUpgradePath = v.Spec.IgnoreUpgradePath
	dst.Spec.ReviveOrder = make([]v1.SubclusterPodCount, len(v.Spec.ReviveOrder))
	for i := range v.Spec.ReviveOrder {
		dst.Spec.ReviveOrder[i] = v1.SubclusterPodCount(v.Spec.ReviveOrder[i])
	}
	dst.Spec.RestartTimeout = v.Spec.RestartTimeout
	dst.Spec.Communal = convertFromCommunal(&v.Spec.Communal)
	dst.Spec.HadoopConfig = v.Spec.Communal.HadoopConfig
	dst.Spec.Local = convertFromLocal(&v.Spec.Local)
	dst.Spec.Subclusters = make([]v1.Subcluster, len(v.Spec.Subclusters))
	for i := range v.Spec.Subclusters {
		dst.Spec.Subclusters[i] = convertFromSubcluster(&v.Spec.Subclusters[i])
	}
	dst.Spec.TemporarySubclusterRouting.Names = v.Spec.TemporarySubclusterRouting.Names
	dst.Spec.TemporarySubclusterRouting.Template = convertFromSubcluster(&v.Spec.TemporarySubclusterRouting.Template)
	dst.Spec.KSafety = v1.KSafetyType(v.Spec.KSafety)
	dst.Spec.RequeueTime = v.Spec.RequeueTime
	dst.Spec.UpgradeRequeueTime = v.Spec.UpgradeRequeueTime
	dst.Spec.Sidecars = v.Spec.Sidecars
	dst.Spec.Volumes = v.Spec.Volumes
	dst.Spec.VolumeMounts = v.Spec.VolumeMounts
	dst.Spec.CertSecrets = convertFromLocalReferenceSlice(v.Spec.CertSecrets)
	dst.Spec.KerberosSecret = v.Spec.KerberosSecret
	dst.Spec.SSHSecret = v.Spec.SSHSecret
	dst.Spec.EncryptSpreadComm = v.Spec.EncryptSpreadComm
	dst.Spec.SecurityContext = v.Spec.SecurityContext
	dst.Spec.PodSecurityContext = v.Spec.PodSecurityContext
	dst.Spec.HTTPServerMode = v1.HTTPServerModeType(v.Spec.HTTPServerMode)
	dst.Spec.HTTPServerTLSSecret = v.Spec.HTTPServerTLSSecret
	dst.Spec.ReadinessProbeOverride = v.Spec.ReadinessProbeOverride
	dst.Spec.LivenessProbeOverride = v.Spec.LivenessProbeOverride
	dst.Spec.StartupProbeOverride = v.Spec.StartupProbeOverride
	return nil
}

// SPILLY - convert status conditions. And anything else in status.

// ConvertFrom will handle conversion from the Hub type (v1) to v1beta1.
//
//nolint:funlen
func (v *VerticaDB) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.VerticaDB)
	verticadblog.Info("ConvertFrom", "name", src.Name, "namespace", src.Namespace, "uid", src.UID)
	v.Name = src.Name
	v.Namespace = src.Namespace
	v.Annotations = src.Annotations
	v.UID = src.UID
	v.Annotations = addConversionAnnotations(v.Annotations)
	v.Labels = src.Labels
	v.Spec.ImagePullPolicy = src.Spec.ImagePullPolicy
	v.Spec.ImagePullSecrets = convertToLocalReferenceSlice(src.Spec.ImagePullSecrets)
	v.Spec.Image = src.Spec.Image
	v.Spec.Labels = src.Spec.Labels
	v.Spec.Annotations = src.Spec.Annotations
	v.Spec.AutoRestartVertica = src.Spec.AutoRestartVertica
	v.Spec.DBName = src.Spec.DBName
	v.Spec.ShardCount = src.Spec.ShardCount
	v.Spec.SuperuserPasswordSecret = src.Spec.SuperuserPasswordSecret
	v.Spec.LicenseSecret = src.Spec.LicenseSecret
	v.Spec.IgnoreClusterLease = src.Spec.IgnoreClusterLease
	v.Spec.InitPolicy = CommunalInitPolicy(src.Spec.InitPolicy)
	v.Spec.UpgradePolicy = UpgradePolicyType(src.Spec.UpgradePolicy)
	v.Spec.IgnoreUpgradePath = src.Spec.IgnoreUpgradePath
	v.Spec.ReviveOrder = make([]SubclusterPodCount, len(src.Spec.ReviveOrder))
	for i := range src.Spec.ReviveOrder {
		v.Spec.ReviveOrder[i] = SubclusterPodCount(src.Spec.ReviveOrder[i])
	}
	v.Spec.RestartTimeout = src.Spec.RestartTimeout
	v.Spec.Communal = convertToCommunal(&src.Spec.Communal)
	v.Spec.Communal.HadoopConfig = src.Spec.HadoopConfig
	v.Spec.Local = convertToLocal(&src.Spec.Local)
	v.Spec.Subclusters = make([]Subcluster, len(src.Spec.Subclusters))
	for i := range src.Spec.Subclusters {
		v.Spec.Subclusters[i] = convertToSubcluster(&src.Spec.Subclusters[i])
	}
	v.Spec.TemporarySubclusterRouting.Names = src.Spec.TemporarySubclusterRouting.Names
	v.Spec.TemporarySubclusterRouting.Template = convertToSubcluster(&src.Spec.TemporarySubclusterRouting.Template)
	v.Spec.KSafety = KSafetyType(src.Spec.KSafety)
	v.Spec.RequeueTime = src.Spec.RequeueTime
	v.Spec.UpgradeRequeueTime = src.Spec.UpgradeRequeueTime
	v.Spec.Sidecars = src.Spec.Sidecars
	v.Spec.Volumes = src.Spec.Volumes
	v.Spec.VolumeMounts = src.Spec.VolumeMounts
	v.Spec.CertSecrets = convertToLocalReferenceSlice(src.Spec.CertSecrets)
	v.Spec.KerberosSecret = src.Spec.KerberosSecret
	v.Spec.SSHSecret = src.Spec.SSHSecret
	v.Spec.EncryptSpreadComm = src.Spec.EncryptSpreadComm
	v.Spec.SecurityContext = src.Spec.SecurityContext
	v.Spec.PodSecurityContext = src.Spec.PodSecurityContext
	v.Spec.HTTPServerMode = HTTPServerModeType(src.Spec.HTTPServerMode)
	v.Spec.HTTPServerTLSSecret = src.Spec.HTTPServerTLSSecret
	v.Spec.ReadinessProbeOverride = src.Spec.ReadinessProbeOverride
	v.Spec.LivenessProbeOverride = src.Spec.LivenessProbeOverride
	v.Spec.StartupProbeOverride = src.Spec.StartupProbeOverride
	return nil
}

// convertFromSubcluster will take a v1beta1 Subcluster and convert it to a v1 version
func convertFromSubcluster(src *Subcluster) v1.Subcluster {
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

// convertToSubcluster will take a v1 Subcluster and convert it to a v1beta1 version
func convertToSubcluster(src *v1.Subcluster) Subcluster {
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

// convertFromCommunal will convert CommunalStorage between v1beta1 to v1 versions
func convertFromCommunal(src *CommunalStorage) v1.CommunalStorage {
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

// SPILLY- make sure convertFrom/convertTo functions make sense given the
// functions they are being called from

// convertToCommunal will convert CommunalStorage between v1beta1 to v1 versions
func convertToCommunal(src *v1.CommunalStorage) CommunalStorage {
	return CommunalStorage{
		Path:                   src.Path,
		IncludeUIDInPath:       src.IncludeUIDInPath,
		Endpoint:               src.Endpoint,
		CredentialSecret:       src.CredentialSecret,
		CaFile:                 src.CaFile,
		Region:                 src.Region,
		KerberosServiceName:    src.KerberosServiceName,
		KerberosRealm:          src.KerberosRealm,
		S3ServerSideEncryption: ServerSideEncryptionType(src.S3ServerSideEncryption),
		S3SseCustomerKeySecret: src.S3SseCustomerKeySecret,
		AdditionalConfig:       src.AdditionalConfig,
	}
}

// convertFromLocal will convert LocalStorage between v1beta1 to v1 versions
func convertFromLocal(src *LocalStorage) v1.LocalStorage {
	return v1.LocalStorage{
		StorageClass: src.StorageClass,
		RequestSize:  src.RequestSize,
		DataPath:     src.DataPath,
		DepotPath:    src.DepotPath,
		DepotVolume:  v1.DepotVolumeType(src.DepotVolume),
		CatalogPath:  src.CatalogPath,
	}
}

// convertToLocal will convert LocalStorage between v1 to v1beta1 versions
func convertToLocal(src *v1.LocalStorage) LocalStorage {
	return LocalStorage{
		StorageClass: src.StorageClass,
		RequestSize:  src.RequestSize,
		DataPath:     src.DataPath,
		DepotPath:    src.DepotPath,
		DepotVolume:  DepotVolumeType(src.DepotVolume),
		CatalogPath:  src.CatalogPath,
	}
}

// convertFromLocalReferenceSlice will convert a []LocalObjectReference from v1beta1
// to v1 versions
func convertFromLocalReferenceSlice(src []LocalObjectReference) []v1.LocalObjectReference {
	dst := make([]v1.LocalObjectReference, len(src))
	for i := range src {
		dst[i] = v1.LocalObjectReference(src[i])
	}
	return dst
}

// convertFromLocalReferenceSlice will convert a []LocalObjectReference from v1
// to v1beta1 versions
func convertToLocalReferenceSlice(src []v1.LocalObjectReference) []LocalObjectReference {
	dst := make([]LocalObjectReference, len(src))
	for i := range src {
		dst[i] = LocalObjectReference(src[i])
	}
	return dst
}

func addConversionAnnotations(annotations map[string]string) map[string]string {
	// Add an annotation so that we know the CR was converted from some other
	// API version.
	if annotations == nil {
		annotations = map[string]string{}
	}
	// SPILLY - this isn't working as you expected
	if _, ok := annotations[vmeta.APIConversionAnnotation]; !ok {
		annotations[vmeta.APIConversionAnnotation] = GroupVersion.Version
	}
	return annotations
}
