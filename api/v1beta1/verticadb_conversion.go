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
	"errors"

	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo is a function to convert a v1beta1 CR to the v1 version of the CR.
func (v *VerticaDB) ConvertTo(dstRaw conversion.Hub) error {
	verticadblog.Info("ConvertTo", "name", v.Name)
	dst := dstRaw.(*v1.VerticaDB)
	dst.Annotations = v.Annotations
	// Add an annotation so that we know the CR was converted from some other
	// API version.
	if dst.Annotations == nil {
		dst.Annotations = map[string]string{}
	}
	if _, ok := v.Annotations[vmeta.ApiConversion]; !ok {
		dst.Annotations[vmeta.ApiConversion] = GroupVersion.Version
	}
	dst.Labels = v.Labels
	dst.Spec.ImagePullPolicy = v.Spec.ImagePullPolicy
	dst.Spec.ImagePullSecrets = convertLocalReferenceSlice(v.Spec.ImagePullSecrets)
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
	dst.Spec.Communal = convertCommunal(&v.Spec.Communal)
	dst.Spec.HadoopConfig = v.Spec.Communal.HadoopConfig
	dst.Spec.Local = convertLocal(&v.Spec.Local)
	dst.Spec.Subclusters = make([]v1.Subcluster, len(v.Spec.Subclusters))
	for i := range v.Spec.Subclusters {
		dst.Spec.Subclusters[i] = convertSubcluster(&v.Spec.Subclusters[i])
	}
	dst.Spec.TemporarySubclusterRouting.Names = v.Spec.TemporarySubclusterRouting.Names
	dst.Spec.TemporarySubclusterRouting.Template = convertSubcluster(&v.Spec.TemporarySubclusterRouting.Template)
	dst.Spec.KSafety = v1.KSafetyType(v.Spec.KSafety)
	dst.Spec.RequeueTime = v.Spec.RequeueTime
	dst.Spec.UpgradeRequeueTime = v.Spec.UpgradeRequeueTime
	dst.Spec.Sidecars = v.Spec.Sidecars
	dst.Spec.Volumes = v.Spec.Volumes
	dst.Spec.VolumeMounts = v.Spec.VolumeMounts
	dst.Spec.CertSecrets = convertLocalReferenceSlice(v.Spec.CertSecrets)
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

// ConvertFrom will handle conversion from the Hub type (v1) to v1beta1.
func (v *VerticaDB) ConvertFrom(_ conversion.Hub) error {
	return errors.New("conversion from v1 to v1beta1 is not implemented")
}

// convertSubcluster will take a v1beta1 Subcluster and convert it to a v1 version
func convertSubcluster(src *Subcluster) v1.Subcluster {
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

// convertCommunal will convert CommunalStorage between v1beta1 to v1 versions
func convertCommunal(src *CommunalStorage) v1.CommunalStorage {
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

// convertLocal will convert LocalStorage between v1beta1 to v1 versions
func convertLocal(src *LocalStorage) v1.LocalStorage {
	return v1.LocalStorage{
		StorageClass: src.StorageClass,
		RequestSize:  src.RequestSize,
		DataPath:     src.DataPath,
		DepotPath:    src.DepotPath,
		DepotVolume:  v1.DepotVolumeType(src.DepotVolume),
		CatalogPath:  src.CatalogPath,
	}
}

// convertLocalReferenceSlice will convert a []LocalObjectReference from v1beta1
// to v1 versions
func convertLocalReferenceSlice(src []LocalObjectReference) []v1.LocalObjectReference {
	dst := make([]v1.LocalObjectReference, len(src))
	for i := range src {
		dst[i] = v1.LocalObjectReference(src[i])
	}
	return dst
}
