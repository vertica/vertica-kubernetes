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

//nolint:dupl
package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
)

var _ = Describe("verticadb_conversion", func() {
	const trueStrVal = "true"
	It("should convert ignoreClusterLease", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}
		v1beta1VDB.Spec.IgnoreClusterLease = true
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.IgnoreClusterLeaseAnnotation]).Should(Equal(trueStrVal))
		v1beta1VDB.Spec.IgnoreClusterLease = false
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.IgnoreClusterLeaseAnnotation]).Should(BeEmpty())
		v1VDB.Annotations[vmeta.IgnoreClusterLeaseAnnotation] = trueStrVal
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.IgnoreClusterLease).Should(BeTrue())
	})

	It("should convert ignoreUpgradePath", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}
		v1beta1VDB.Spec.IgnoreUpgradePath = true
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.IgnoreUpgradePathAnnotation]).Should(Equal(trueStrVal))
		v1beta1VDB.Spec.IgnoreUpgradePath = false
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.IgnoreUpgradePathAnnotation]).Should(BeEmpty())
		v1VDB.Annotations[vmeta.IgnoreUpgradePathAnnotation] = trueStrVal
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.IgnoreUpgradePath).Should(BeTrue())
	})

	It("should convert RestartTimeout", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}
		v1beta1VDB.Spec.RestartTimeout = 55
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.RestartTimeoutAnnotation]).Should(Equal("55"))
		v1VDB.Annotations[vmeta.RestartTimeoutAnnotation] = "88"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.RestartTimeout).Should(Equal(88))
	})

	It("should convert temporarySubclusterRouting", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Spec.TemporarySubclusterRouting).Should(BeNil())

		v1beta1VDB.Spec.TemporarySubclusterRouting.Names = []string{"s1", "s2"}
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Spec.TemporarySubclusterRouting).ShouldNot(BeNil())
		Ω(v1VDB.Spec.TemporarySubclusterRouting.Names).Should(HaveLen(2))
		Ω(v1VDB.Spec.TemporarySubclusterRouting.Names).Should(ContainElements("s1", "s2"))

		v1VDB.Spec.TemporarySubclusterRouting.Names = []string{}
		const transientSCName = "transient-1"
		const transientSCSize = 3
		v1VDB.Spec.TemporarySubclusterRouting.Template = v1.Subcluster{
			Name: transientSCName,
			Size: transientSCSize,
		}
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.TemporarySubclusterRouting.Names).Should(HaveLen(0))
		Ω(v1beta1VDB.Spec.TemporarySubclusterRouting.Template.Name).Should(Equal(transientSCName))
		Ω(v1beta1VDB.Spec.TemporarySubclusterRouting.Template.Size).Should(Equal(int32(transientSCSize)))
	})

	It("should convert kSafety", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		v1beta1VDB.Spec.KSafety = KSafety0
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.KSafetyAnnotation]).Should(Equal("0"))
		v1beta1VDB.Spec.KSafety = KSafety1
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.KSafetyAnnotation]).Should(BeEmpty())

		// v1 -> v1beta1
		v1VDB.Annotations[vmeta.KSafetyAnnotation] = "0"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.KSafety).Should(Equal(KSafety0))
		v1VDB.Annotations[vmeta.KSafetyAnnotation] = "1"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.KSafety).Should(Equal(KSafety1))
		v1VDB.Annotations[vmeta.KSafetyAnnotation] = "huh"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.KSafety).Should(Equal(KSafety1))
	})

	It("should convert requeueTime", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		v1beta1VDB.Spec.RequeueTime = 33
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.RequeueTimeAnnotation]).Should(Equal("33"))
		v1beta1VDB.Spec.RequeueTime = 0
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.RequeueTimeAnnotation]).Should(BeEmpty())

		// v1 -> v1beta1
		v1VDB.Annotations[vmeta.RequeueTimeAnnotation] = "13"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.RequeueTime).Should(Equal(13))
	})

	It("should convert upgradeRequeueTime", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		v1beta1VDB.Spec.UpgradeRequeueTime = 60
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.UpgradeRequeueTimeAnnotation]).Should(Equal("60"))
		v1beta1VDB.Spec.UpgradeRequeueTime = 0
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.UpgradeRequeueTimeAnnotation]).Should(BeEmpty())

		// v1 -> v1beta1
		v1VDB.Annotations[vmeta.UpgradeRequeueTimeAnnotation] = "75"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.UpgradeRequeueTime).Should(Equal(75))
	})

	It("should convert sshSecret", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		v1beta1VDB.Spec.SSHSecret = "s1"
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.SSHSecAnnotation]).Should(Equal("s1"))
		v1beta1VDB.Spec.SSHSecret = ""
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.SSHSecAnnotation]).Should(BeEmpty())

		// v1 -> v1beta1
		v1VDB.Annotations[vmeta.SSHSecAnnotation] = "s2"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.SSHSecret).Should(Equal("s2"))
	})

	It("should convert includeUIDInPath", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		v1beta1VDB.Spec.Communal.IncludeUIDInPath = true
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.IncludeUIDInPathAnnotation]).Should(Equal("true"))
		v1beta1VDB.Spec.Communal.IncludeUIDInPath = false
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.IncludeUIDInPathAnnotation]).Should(BeEmpty())

		// v1 -> v1beta1
		v1VDB.Annotations[vmeta.IncludeUIDInPathAnnotation] = "true"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.Communal.IncludeUIDInPath).Should(BeTrue())
	})

	It("should convert kerberos fields", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		v1beta1VDB.Spec.Communal.KerberosRealm = "krealm"
		v1beta1VDB.Spec.Communal.KerberosServiceName = "kservice"
		v1beta1VDB.Spec.Communal.AdditionalConfig = make(map[string]string)
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Spec.Communal.AdditionalConfig[vmeta.KerberosRealmConfig]).Should(Equal("krealm"))
		Ω(v1VDB.Spec.Communal.AdditionalConfig[vmeta.KerberosServiceNameConfig]).Should(Equal("kservice"))

		// v1 -> v1beta1
		v1VDB.Spec.Communal.AdditionalConfig[vmeta.KerberosRealmConfig] = "new-krealm"
		v1VDB.Spec.Communal.AdditionalConfig[vmeta.KerberosServiceNameConfig] = "new-kservice"
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.Communal.KerberosRealm).Should(Equal("new-krealm"))
		Ω(v1beta1VDB.Spec.Communal.KerberosServiceName).Should(Equal("new-kservice"))
	})

	It("should convert between subcluster type", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		v1beta1VDB.Spec.Subclusters[0].IsPrimary = true
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Spec.Subclusters[0].Type).Should(Equal(v1.PrimarySubcluster))
		v1beta1VDB.Spec.Subclusters[0].IsPrimary = false
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Spec.Subclusters[0].Type).Should(Equal(v1.SecondarySubcluster))

		// v1 -> v1beta1
		v1VDB.Spec.Subclusters[0].Type = v1.PrimarySubcluster
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.Subclusters[0].IsPrimary).Should(BeTrue())
		v1VDB.Spec.Subclusters[0].Type = v1.SecondarySubcluster
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.Subclusters[0].IsPrimary).Should(BeFalse())
	})

	It("should convert isTransient", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		v1beta1VDB.Spec.Subclusters[0].IsPrimary = false
		v1beta1VDB.Spec.Subclusters[0].IsTransient = true
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Spec.Subclusters[0].Type).Should(Equal(v1.TransientSubcluster))

		// v1 -> v1beta1
		v1VDB.Spec.Subclusters[0].Type = v1.TransientSubcluster
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Spec.Subclusters[0].IsPrimary).Should(BeFalse())
		Ω(v1beta1VDB.Spec.Subclusters[0].IsTransient).Should(BeTrue())
	})

	It("should convert installCount in status field", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		v1beta1VDB.Status.InstallCount = 3
		v1beta1VDB.Status.Subclusters = []SubclusterStatus{
			{
				InstallCount: 1,
				Detail:       []VerticaDBPodStatus{{Installed: true}},
			},
			{
				InstallCount: 2,
				Detail:       []VerticaDBPodStatus{{Installed: true}, {Installed: true}},
			},
		}
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Status.InstallCount()).Should(Equal(int32(3)))
		Ω(v1VDB.Status.Subclusters).Should(HaveLen(2))
		Ω(v1VDB.Status.Subclusters[0].InstallCount()).Should(Equal(int32(1)))
		Ω(v1VDB.Status.Subclusters[1].InstallCount()).Should(Equal(int32(2)))

		// v1 -> v1beta1
		v1VDB.Status.Subclusters = []v1.SubclusterStatus{
			{
				Detail: []v1.VerticaDBPodStatus{{Installed: true}, {Installed: true}, {Installed: true}},
			},
			{
				Detail: []v1.VerticaDBPodStatus{{Installed: true}},
			},
			{
				Detail: []v1.VerticaDBPodStatus{{Installed: false}},
			},
		}
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Status.InstallCount).Should(Equal(int32(4)))
		Ω(v1beta1VDB.Status.Subclusters).Should(HaveLen(3))
		Ω(v1beta1VDB.Status.Subclusters[0].InstallCount).Should(Equal(int32(3)))
		Ω(v1beta1VDB.Status.Subclusters[1].InstallCount).Should(Equal(int32(1)))
		Ω(v1beta1VDB.Status.Subclusters[2].InstallCount).Should(Equal(int32(0)))
	})

	It("should convert condition names", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		v1beta1VDB.Status.Conditions = []VerticaDBCondition{
			{Type: ImageChangeInProgress},
		}
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Status.Conditions).Should(HaveLen(1))
		Ω(v1VDB.Status.Conditions[0].Type).Should(Equal(v1.UpgradeInProgress))

		// v1 -> v1beta1
		v1beta1VDB.Status.Conditions = nil
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Status.Conditions).Should(HaveLen(1))
		Ω(v1beta1VDB.Status.Conditions[0].Type).Should(Equal(ImageChangeInProgress))
	})

	It("should convert nma running mode", func() {
		v1beta1VDB := MakeVDB()
		v1VDB := v1.VerticaDB{}

		// v1beta1 -> v1
		// test VClusterOpsAnnotation is true
		v1beta1VDB.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		Ω(v1beta1VDB.ConvertTo(&v1VDB)).Should(Succeed())
		Ω(v1VDB.Annotations[vmeta.RunNMAInSidecarAnnotation]).Should(Equal(vmeta.RunNMAInSidecarAnnotationFalse))

		// v1 -> v1beta1
		// test VClusterOpsAnnotation is true
		// remove RunNMAInSidecarAnnotation in src
		delete(v1VDB.Annotations, vmeta.RunNMAInSidecarAnnotation)
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Annotations[vmeta.RunNMAInSidecarAnnotation]).Should(Equal(vmeta.RunNMAInSidecarAnnotationTrue))
		// test VClusterOpsAnnotation is not present
		delete(v1VDB.Annotations, vmeta.VClusterOpsAnnotation)
		// reset dest annotation by removing RunNMAInSidecarAnnotation
		delete(v1beta1VDB.Annotations, vmeta.RunNMAInSidecarAnnotation)
		Ω(v1beta1VDB.ConvertFrom(&v1VDB)).Should(Succeed())
		Ω(v1beta1VDB.Annotations[vmeta.RunNMAInSidecarAnnotation]).Should(Equal(vmeta.RunNMAInSidecarAnnotationTrue))
	})
})
