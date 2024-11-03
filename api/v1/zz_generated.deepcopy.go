//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Affinity) DeepCopyInto(out *Affinity) {
	*out = *in
	if in.NodeAffinity != nil {
		in, out := &in.NodeAffinity, &out.NodeAffinity
		*out = new(corev1.NodeAffinity)
		(*in).DeepCopyInto(*out)
	}
	if in.PodAffinity != nil {
		in, out := &in.PodAffinity, &out.PodAffinity
		*out = new(corev1.PodAffinity)
		(*in).DeepCopyInto(*out)
	}
	if in.PodAntiAffinity != nil {
		in, out := &in.PodAntiAffinity, &out.PodAntiAffinity
		*out = new(corev1.PodAntiAffinity)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Affinity.
func (in *Affinity) DeepCopy() *Affinity {
	if in == nil {
		return nil
	}
	out := new(Affinity)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CommunalStorage) DeepCopyInto(out *CommunalStorage) {
	*out = *in
	if in.AdditionalConfig != nil {
		in, out := &in.AdditionalConfig, &out.AdditionalConfig
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CommunalStorage.
func (in *CommunalStorage) DeepCopy() *CommunalStorage {
	if in == nil {
		return nil
	}
	out := new(CommunalStorage)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LocalObjectReference) DeepCopyInto(out *LocalObjectReference) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LocalObjectReference.
func (in *LocalObjectReference) DeepCopy() *LocalObjectReference {
	if in == nil {
		return nil
	}
	out := new(LocalObjectReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LocalStorage) DeepCopyInto(out *LocalStorage) {
	*out = *in
	out.RequestSize = in.RequestSize.DeepCopy()
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LocalStorage.
func (in *LocalStorage) DeepCopy() *LocalStorage {
	if in == nil {
		return nil
	}
	out := new(LocalStorage)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Proxy) DeepCopyInto(out *Proxy) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Proxy.
func (in *Proxy) DeepCopy() *Proxy {
	if in == nil {
		return nil
	}
	out := new(Proxy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RestorePointInfo) DeepCopyInto(out *RestorePointInfo) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RestorePointInfo.
func (in *RestorePointInfo) DeepCopy() *RestorePointInfo {
	if in == nil {
		return nil
	}
	out := new(RestorePointInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RestorePointPolicy) DeepCopyInto(out *RestorePointPolicy) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RestorePointPolicy.
func (in *RestorePointPolicy) DeepCopy() *RestorePointPolicy {
	if in == nil {
		return nil
	}
	out := new(RestorePointPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Sandbox) DeepCopyInto(out *Sandbox) {
	*out = *in
	if in.Subclusters != nil {
		in, out := &in.Subclusters, &out.Subclusters
		*out = make([]SubclusterName, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Sandbox.
func (in *Sandbox) DeepCopy() *Sandbox {
	if in == nil {
		return nil
	}
	out := new(Sandbox)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SandboxStatus) DeepCopyInto(out *SandboxStatus) {
	*out = *in
	if in.Subclusters != nil {
		in, out := &in.Subclusters, &out.Subclusters
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	out.UpgradeState = in.UpgradeState
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SandboxStatus.
func (in *SandboxStatus) DeepCopy() *SandboxStatus {
	if in == nil {
		return nil
	}
	out := new(SandboxStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SandboxUpgradeState) DeepCopyInto(out *SandboxUpgradeState) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SandboxUpgradeState.
func (in *SandboxUpgradeState) DeepCopy() *SandboxUpgradeState {
	if in == nil {
		return nil
	}
	out := new(SandboxUpgradeState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Subcluster) DeepCopyInto(out *Subcluster) {
	*out = *in
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	in.Affinity.DeepCopyInto(&out.Affinity)
	if in.Tolerations != nil {
		in, out := &in.Tolerations, &out.Tolerations
		*out = make([]corev1.Toleration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.Resources.DeepCopyInto(&out.Resources)
	if in.ExternalIPs != nil {
		in, out := &in.ExternalIPs, &out.ExternalIPs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ServiceAnnotations != nil {
		in, out := &in.ServiceAnnotations, &out.ServiceAnnotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Proxy != nil {
		in, out := &in.Proxy, &out.Proxy
		*out = make([]Proxy, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Subcluster.
func (in *Subcluster) DeepCopy() *Subcluster {
	if in == nil {
		return nil
	}
	out := new(Subcluster)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubclusterName) DeepCopyInto(out *SubclusterName) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubclusterName.
func (in *SubclusterName) DeepCopy() *SubclusterName {
	if in == nil {
		return nil
	}
	out := new(SubclusterName)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubclusterPodCount) DeepCopyInto(out *SubclusterPodCount) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubclusterPodCount.
func (in *SubclusterPodCount) DeepCopy() *SubclusterPodCount {
	if in == nil {
		return nil
	}
	out := new(SubclusterPodCount)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubclusterSelection) DeepCopyInto(out *SubclusterSelection) {
	*out = *in
	if in.Names != nil {
		in, out := &in.Names, &out.Names
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	in.Template.DeepCopyInto(&out.Template)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubclusterSelection.
func (in *SubclusterSelection) DeepCopy() *SubclusterSelection {
	if in == nil {
		return nil
	}
	out := new(SubclusterSelection)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubclusterStatus) DeepCopyInto(out *SubclusterStatus) {
	*out = *in
	if in.Detail != nil {
		in, out := &in.Detail, &out.Detail
		*out = make([]VerticaDBPodStatus, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubclusterStatus.
func (in *SubclusterStatus) DeepCopy() *SubclusterStatus {
	if in == nil {
		return nil
	}
	out := new(SubclusterStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VerticaDB) DeepCopyInto(out *VerticaDB) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VerticaDB.
func (in *VerticaDB) DeepCopy() *VerticaDB {
	if in == nil {
		return nil
	}
	out := new(VerticaDB)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *VerticaDB) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VerticaDBList) DeepCopyInto(out *VerticaDBList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]VerticaDB, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VerticaDBList.
func (in *VerticaDBList) DeepCopy() *VerticaDBList {
	if in == nil {
		return nil
	}
	out := new(VerticaDBList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *VerticaDBList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VerticaDBPodStatus) DeepCopyInto(out *VerticaDBPodStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VerticaDBPodStatus.
func (in *VerticaDBPodStatus) DeepCopy() *VerticaDBPodStatus {
	if in == nil {
		return nil
	}
	out := new(VerticaDBPodStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VerticaDBSpec) DeepCopyInto(out *VerticaDBSpec) {
	*out = *in
	if in.ImagePullSecrets != nil {
		in, out := &in.ImagePullSecrets, &out.ImagePullSecrets
		*out = make([]LocalObjectReference, len(*in))
		copy(*out, *in)
	}
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.RestorePoint != nil {
		in, out := &in.RestorePoint, &out.RestorePoint
		*out = new(RestorePointPolicy)
		**out = **in
	}
	if in.ReviveOrder != nil {
		in, out := &in.ReviveOrder, &out.ReviveOrder
		*out = make([]SubclusterPodCount, len(*in))
		copy(*out, *in)
	}
	in.Communal.DeepCopyInto(&out.Communal)
	in.Local.DeepCopyInto(&out.Local)
	if in.Subclusters != nil {
		in, out := &in.Subclusters, &out.Subclusters
		*out = make([]Subcluster, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.TemporarySubclusterRouting != nil {
		in, out := &in.TemporarySubclusterRouting, &out.TemporarySubclusterRouting
		*out = new(SubclusterSelection)
		(*in).DeepCopyInto(*out)
	}
	if in.Sidecars != nil {
		in, out := &in.Sidecars, &out.Sidecars
		*out = make([]corev1.Container, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Volumes != nil {
		in, out := &in.Volumes, &out.Volumes
		*out = make([]corev1.Volume, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.VolumeMounts != nil {
		in, out := &in.VolumeMounts, &out.VolumeMounts
		*out = make([]corev1.VolumeMount, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.CertSecrets != nil {
		in, out := &in.CertSecrets, &out.CertSecrets
		*out = make([]LocalObjectReference, len(*in))
		copy(*out, *in)
	}
	if in.SecurityContext != nil {
		in, out := &in.SecurityContext, &out.SecurityContext
		*out = new(corev1.SecurityContext)
		(*in).DeepCopyInto(*out)
	}
	if in.PodSecurityContext != nil {
		in, out := &in.PodSecurityContext, &out.PodSecurityContext
		*out = new(corev1.PodSecurityContext)
		(*in).DeepCopyInto(*out)
	}
	if in.ReadinessProbeOverride != nil {
		in, out := &in.ReadinessProbeOverride, &out.ReadinessProbeOverride
		*out = new(corev1.Probe)
		(*in).DeepCopyInto(*out)
	}
	if in.LivenessProbeOverride != nil {
		in, out := &in.LivenessProbeOverride, &out.LivenessProbeOverride
		*out = new(corev1.Probe)
		(*in).DeepCopyInto(*out)
	}
	if in.StartupProbeOverride != nil {
		in, out := &in.StartupProbeOverride, &out.StartupProbeOverride
		*out = new(corev1.Probe)
		(*in).DeepCopyInto(*out)
	}
	if in.Sandboxes != nil {
		in, out := &in.Sandboxes, &out.Sandboxes
		*out = make([]Sandbox, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VerticaDBSpec.
func (in *VerticaDBSpec) DeepCopy() *VerticaDBSpec {
	if in == nil {
		return nil
	}
	out := new(VerticaDBSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VerticaDBStatus) DeepCopyInto(out *VerticaDBStatus) {
	*out = *in
	if in.Subclusters != nil {
		in, out := &in.Subclusters, &out.Subclusters
		*out = make([]SubclusterStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Sandboxes != nil {
		in, out := &in.Sandboxes, &out.Sandboxes
		*out = make([]SandboxStatus, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.RestorePoint != nil {
		in, out := &in.RestorePoint, &out.RestorePoint
		*out = new(RestorePointInfo)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VerticaDBStatus.
func (in *VerticaDBStatus) DeepCopy() *VerticaDBStatus {
	if in == nil {
		return nil
	}
	out := new(VerticaDBStatus)
	in.DeepCopyInto(out)
	return out
}
