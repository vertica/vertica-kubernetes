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

package builder

import (
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("builder", func() {
	It("should generate identical k8s containers each time", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Annotations = map[string]string{
			"key1":                     "val1",
			"key2":                     "val2",
			"vertica.com/gitRef":       "abcd123",
			"1_not_valid_env_var_name": "blah",
		}

		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		const MaxLoopIteratons = 100
		for i := 1; i < MaxLoopIteratons; i++ {
			c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
			Ω(reflect.DeepEqual(c, baseContainer)).Should(BeTrue())
		}
	})

	It("should add our own capabilities to the securityContext for admintools only", func() {
		vdb := vapi.MakeVDB()
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(baseContainer.SecurityContext).ShouldNot(BeNil())
		Ω(baseContainer.SecurityContext.Capabilities).ShouldNot(BeNil())
		Ω(baseContainer.SecurityContext.Capabilities.Add).Should(ContainElements([]v1.Capability{"SYS_CHROOT", "AUDIT_WRITE"}))

		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Spec.SecurityContext = &v1.SecurityContext{
			Capabilities: &v1.Capabilities{},
		}
		baseContainer = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(baseContainer.SecurityContext).ShouldNot(BeNil())
		Ω(baseContainer.SecurityContext.Capabilities.Add).ShouldNot(ContainElement([]v1.Capability{"SYS_CHROOT"}))
		Ω(baseContainer.SecurityContext.Capabilities.Add).ShouldNot(ContainElement([]v1.Capability{"AUDIT_WRITE"}))

	})

	It("should add omit our own capabilities in the securityContext if we are dropping them", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.SecurityContext = &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Drop: []v1.Capability{"AUDIT_WRITE"},
			},
		}
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(baseContainer.SecurityContext).ShouldNot(BeNil())
		Ω(baseContainer.SecurityContext.Capabilities).ShouldNot(BeNil())
		Ω(baseContainer.SecurityContext.Capabilities.Add).Should(ContainElements([]v1.Capability{"SYS_CHROOT"}))
		Ω(baseContainer.SecurityContext.Capabilities.Add).ShouldNot(ContainElement([]v1.Capability{"AUDIT_WRITE"}))
	})

	It("should allow you to run in priv mode", func() {
		vdb := vapi.MakeVDB()
		priv := true
		vdb.Spec.SecurityContext = &v1.SecurityContext{
			Privileged: &priv,
		}
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(baseContainer.SecurityContext).ShouldNot(BeNil())
		Ω(baseContainer.SecurityContext.Privileged).ShouldNot(BeNil())
		Ω(*baseContainer.SecurityContext.Privileged).Should(BeTrue())
	})

	It("should add a catalog mount point if it differs from data", func() {
		vdb := vapi.MakeVDB()
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("catalog")))
		vdb.Spec.Local.CatalogPath = "/catalog"
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("catalog")))
	})

	It("should only have separate mount paths for data, depot and catalog if they are different", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Local.DataPath = "/vertica"
		vdb.Spec.Local.DepotPath = vdb.Spec.Local.DataPath
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("catalog")))
		Ω(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("depot")))
		Ω(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("data")))
		vdb.Spec.Local.DepotPath = "/depot"
		vdb.Spec.Local.CatalogPath = vdb.Spec.Local.DepotPath
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("depot")))
		Ω(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("catalog")))
		Ω(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("data")))
		vdb.Spec.Local.CatalogPath = "/vertica/catalog"
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("catalog")))
	})

	It("should have a specific mount name and no subPath for depot if depotVolume is EmptyDir", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Local.DepotVolume = vapi.PersistentVolume
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(makeVolumeMountNames(&c)).ShouldNot(ContainElement(ContainSubstring(vapi.DepotMountName)))
		Ω(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("depot")))
		vdb.Spec.Local.DepotVolume = vapi.EmptyDir
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(makeVolumeMountNames(&c)).Should(ContainElement(ContainSubstring(vapi.DepotMountName)))
		Ω(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("depot")))
	})

	It("should allow parts of the readiness probe to be overridden", func() {
		vdb := vapi.MakeVDB()
		NewCommand := []string{"new", "command"}
		const NewTimeout int32 = 5
		const NewFailureThreshold int32 = 6
		const NewInitialDelaySeconds int32 = 7
		const NewPeriodSeconds int32 = 8
		const NewSuccessThreshold int32 = 9
		vdb.Spec.ReadinessProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				Exec: &v1.ExecAction{
					Command: NewCommand,
				},
			},
			TimeoutSeconds:      NewTimeout,
			FailureThreshold:    NewFailureThreshold,
			InitialDelaySeconds: NewInitialDelaySeconds,
			PeriodSeconds:       NewPeriodSeconds,
			SuccessThreshold:    NewSuccessThreshold,
		}
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(c.ReadinessProbe.Exec.Command).Should(Equal(NewCommand))
		Ω(c.ReadinessProbe.TimeoutSeconds).Should(Equal(NewTimeout))
		Ω(c.ReadinessProbe.FailureThreshold).Should(Equal(NewFailureThreshold))
		Ω(c.ReadinessProbe.InitialDelaySeconds).Should(Equal(NewInitialDelaySeconds))
		Ω(c.ReadinessProbe.PeriodSeconds).Should(Equal(NewPeriodSeconds))
		Ω(c.ReadinessProbe.SuccessThreshold).Should(Equal(NewSuccessThreshold))
	})

	It("should allow parts of the startupProbe and livenessProbe to be overridden", func() {
		vdb := vapi.MakeVDB()
		const NewTimeout int32 = 10
		const NewPeriodSeconds int32 = 20
		vdb.Spec.LivenessProbeOverride = &v1.Probe{
			TimeoutSeconds: NewTimeout,
		}
		vdb.Spec.StartupProbeOverride = &v1.Probe{
			PeriodSeconds: NewPeriodSeconds,
		}
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(c.LivenessProbe.TimeoutSeconds).Should(Equal(NewTimeout))
		Ω(c.StartupProbe.PeriodSeconds).Should(Equal(NewPeriodSeconds))
	})

	It("should have all probes use the http version endpoint", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue

		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(c.ReadinessProbe.HTTPGet.Path).Should(Equal(HTTPServerVersionPath))
		Ω(c.ReadinessProbe.HTTPGet.Port).Should(Equal(intstr.FromInt(VerticaHTTPPort)))
		Ω(c.ReadinessProbe.HTTPGet.Scheme).Should(Equal(v1.URISchemeHTTPS))
		Ω(c.LivenessProbe.HTTPGet.Path).Should(Equal(HTTPServerVersionPath))
		Ω(c.LivenessProbe.HTTPGet.Port).Should(Equal(intstr.FromInt(VerticaHTTPPort)))
		Ω(c.LivenessProbe.HTTPGet.Scheme).Should(Equal(v1.URISchemeHTTPS))
		Ω(c.StartupProbe.HTTPGet.Path).Should(Equal(HTTPServerVersionPath))
		Ω(c.StartupProbe.HTTPGet.Port).Should(Equal(intstr.FromInt(VerticaHTTPPort)))
		Ω(c.StartupProbe.HTTPGet.Scheme).Should(Equal(v1.URISchemeHTTPS))
	})

	It("should not mount superuser password", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.PasswordSecret = "some-secret"

		// case 1:  if probe's overridden
		vdb.Spec.ReadinessProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				TCPSocket: &v1.TCPSocketAction{
					Port: intstr.FromInt(5433),
				},
			},
		}
		vdb.Spec.StartupProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				Exec: &v1.ExecAction{
					Command: []string{"vsql", "-c", "select 1"},
				},
			},
		}
		c := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(isPasswdIncludedInPodInfo(vdb, &c)).Should(BeFalse())

		vdb.Spec.StartupProbeOverride = nil

		// case 2: if in vclusterops mode
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		c = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(isPasswdIncludedInPodInfo(vdb, &c)).Should(BeFalse())

		// case 3: should mount
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		c = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(isPasswdIncludedInPodInfo(vdb, &c)).Should(BeTrue())
	})

	It("should mount startup vol only when nma sidecar mode", func() {
		vdb := vapi.MakeVDB()

		vdb.Annotations[vmeta.RunNMAInSidecarAnnotation] = vmeta.RunNMAInSidecarAnnotationFalse
		c := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(getStartupConfVolume(c.Volumes)).Should(BeNil())

		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		c = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(getStartupConfVolume(c.Volumes)).Should(BeNil())

		vdb.Annotations[vmeta.RunNMAInSidecarAnnotation] = vmeta.RunNMAInSidecarAnnotationTrue
		c = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(getStartupConfVolume(c.Volumes)).ShouldNot(BeNil())
	})

	It("should allow override of probe with grpc and httpget", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.ReadinessProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				GRPC: &v1.GRPCAction{
					Port: 5433,
				},
			},
		}
		vdb.Spec.LivenessProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/health",
				},
			},
		}
		c := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(c.Containers[0].ReadinessProbe.Exec).Should(BeNil())
		Ω(c.Containers[0].ReadinessProbe.GRPC).ShouldNot(BeNil())
		Ω(c.Containers[0].LivenessProbe.Exec).Should(BeNil())
		Ω(c.Containers[0].LivenessProbe.HTTPGet).ShouldNot(BeNil())
	})

	It("should override readiness and liveness probes when vclusterops is enabled", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue

		c := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		inx := names.GetServerContainerIndex(vdb)
		Ω(c.Containers[inx].ReadinessProbe.HTTPGet).ShouldNot(BeNil())
		Ω(c.Containers[inx].LivenessProbe.HTTPGet).ShouldNot(BeNil())

		vdb.Spec.ReadinessProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				Exec: &v1.ExecAction{
					Command: []string{"vsql", "-c", "select 1"},
				},
			},
		}
		vdb.Spec.LivenessProbeOverride = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				TCPSocket: &v1.TCPSocketAction{
					Port: intstr.FromInt(VerticaClientPort),
				},
			},
		}
		c = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(c.Containers[inx].ReadinessProbe.HTTPGet).Should(BeNil())
		Ω(c.Containers[inx].LivenessProbe.HTTPGet).Should(BeNil())
		Ω(c.Containers[inx].ReadinessProbe.Exec).ShouldNot(BeNil())
		Ω(c.Containers[inx].LivenessProbe.TCPSocket).ShouldNot(BeNil())
	})

	It("should not use canary query probe if using GSM", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.PasswordSecret = "gsm://project/team/dbadmin/secret/1"
		vdb.Spec.Communal.Path = "gs://vertica-fleeting/mydb"
		c := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(isPasswdIncludedInPodInfo(vdb, &c)).Should(BeFalse())
	})

	It("should override some of the pod securityContext settings", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.PodSecurityContext = &v1.PodSecurityContext{
			Sysctls: []v1.Sysctl{
				{Name: "net.ipv4.tcp_keepalive_time", Value: "45"},
				{Name: "net.ipv4.tcp_keepalive_intvl", Value: "5"},
			},
		}
		c := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		Ω(len(c.SecurityContext.Sysctls)).Should(Equal(2))
		Ω(c.SecurityContext.Sysctls[0].Name).Should(Equal("net.ipv4.tcp_keepalive_time"))
		Ω(c.SecurityContext.Sysctls[0].Value).Should(Equal("45"))
		Ω(c.SecurityContext.Sysctls[1].Name).Should(Equal("net.ipv4.tcp_keepalive_intvl"))
		Ω(c.SecurityContext.Sysctls[1].Value).Should(Equal("5"))
	})

	It("should mount ssh secret for dbadmin and root", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.SSHSecAnnotation] = "my-secret"
		c := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		cnt := &c.Containers[0]
		i, ok := getFirstSSHSecretVolumeMountIndex(cnt)
		Ω(ok).Should(BeTrue())
		const ΩedPathsPerMount = 3
		Ω(len(cnt.VolumeMounts)).Should(BeNumerically(">=", i+2*ΩedPathsPerMount))
		for j := 0; i < ΩedPathsPerMount; i++ {
			Ω(cnt.VolumeMounts[i+j].MountPath).Should(ContainSubstring(paths.DBAdminSSHPath))
		}
		for j := 0; i < ΩedPathsPerMount; i++ {
			Ω(cnt.VolumeMounts[i+ΩedPathsPerMount+j].MountPath).Should(ContainSubstring(paths.RootSSHPath))
		}
	})

	It("should mount or not mount NMA certs volume according to annotation(monolithic)", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		// monolithic container
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Annotations[vmeta.RunNMAInSidecarAnnotation] = vmeta.RunNMAInSidecarAnnotationFalse
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = vmeta.MountNMACertsAnnotationFalse
		ps := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeFalse())
		Ω(NMACertsVolumeMountExists(&c)).Should(BeFalse())
		Ω(NMACertsEnvVarsExist(vdb, &c)).Should(BeTrue())
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = vmeta.MountNMACertsAnnotationTrue
		ps = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeTrue())
		Ω(NMACertsVolumeMountExists(&c)).Should(BeTrue())
		Ω(NMACertsEnvVarsExist(vdb, &c)).Should(BeTrue())
		// test default value (which should be true)
		delete(vdb.Annotations, vmeta.MountNMACertsAnnotation)
		ps = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeTrue())
		Ω(NMACertsVolumeMountExists(&c)).Should(BeTrue())
		Ω(NMACertsEnvVarsExist(vdb, &c)).Should(BeTrue())
	})

	It("should mount or not mount NMA certs volume according to annotation(sidecar)", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")

		// server container
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Annotations[vmeta.RunNMAInSidecarAnnotation] = vmeta.RunNMAInSidecarAnnotationTrue
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = vmeta.MountNMACertsAnnotationFalse
		ps := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeFalse())
		Ω(NMACertsVolumeMountExists(&c)).Should(BeFalse())
		Ω(NMACertsEnvVarsExist(vdb, &c)).Should(BeFalse())
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = vmeta.MountNMACertsAnnotationTrue
		ps = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeTrue())
		Ω(NMACertsVolumeMountExists(&c)).Should(BeFalse())
		Ω(NMACertsEnvVarsExist(vdb, &c)).Should(BeFalse())

		// nma container
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = vmeta.MountNMACertsAnnotationFalse
		ps = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c = makeNMAContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeFalse())
		Ω(NMACertsVolumeMountExists(&c)).Should(BeFalse())
		Ω(NMACertsEnvVarsExist(vdb, &c)).Should(BeTrue())
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = vmeta.MountNMACertsAnnotationTrue
		ps = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c = makeNMAContainer(vdb, &vdb.Spec.Subclusters[0])
		Ω(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeTrue())
		Ω(NMACertsVolumeMountExists(&c)).Should(BeTrue())
		Ω(NMACertsEnvVarsExist(vdb, &c)).Should(BeTrue())
	})

	It("should not set any NMA resources if none are set for the subcluster", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		sc := &vdb.Spec.Subclusters[0]
		sc.Resources = v1.ResourceRequirements{}
		nma := makeNMAContainer(vdb, sc)
		_, ok := nma.Resources.Limits[v1.ResourceCPU]
		Ω(ok).Should(BeFalse())
		_, ok = nma.Resources.Limits[v1.ResourceMemory]
		Ω(ok).Should(BeFalse())
		_, ok = nma.Resources.Requests[v1.ResourceCPU]
		Ω(ok).Should(BeFalse())
		_, ok = nma.Resources.Requests[v1.ResourceMemory]
		Ω(ok).Should(BeFalse())
	})

	It("should set NMA resources if forced too", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		vdb.Annotations[vmeta.NMAResourcesForcedAnnotation] = "1"
		vdb.Annotations[vmeta.GenNMAResourcesAnnotationName(v1.ResourceLimitsCPU)] = "4"
		// Intentionally leave cpu request out so that we will use and check the default
		vdb.Annotations[vmeta.GenNMAResourcesAnnotationName(v1.ResourceRequestsMemory)] = "250Mi"
		vdb.Annotations[vmeta.GenNMAResourcesAnnotationName(v1.ResourceLimitsMemory)] = "750Mi"
		sc := &vdb.Spec.Subclusters[0]
		sc.Resources = v1.ResourceRequirements{}
		nma := makeNMAContainer(vdb, sc)
		actual, _ := nma.Resources.Limits.Cpu().AsInt64()
		Ω(actual).Should(Equal(int64(4)))
		actual, _ = nma.Resources.Requests.Cpu().AsInt64()
		defQuantity := vmeta.DefaultNMAResources[v1.ResourceRequestsCPU]
		defVal, _ := defQuantity.AsInt64()
		Ω(actual).Should(Equal(defVal))
		actual, _ = nma.Resources.Limits.Memory().AsInt64()
		Ω(actual).Should(Equal(int64(750 * 1024 * 1024)))
		actual, _ = nma.Resources.Requests.Memory().AsInt64()
		Ω(actual).Should(Equal(int64(250 * 1024 * 1024)))
	})

	It("should set NMA resources if set in the server", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		vdb.Annotations[vmeta.GenNMAResourcesAnnotationName(v1.ResourceLimitsCPU)] = "8"
		vdb.Annotations[vmeta.GenNMAResourcesAnnotationName(v1.ResourceLimitsMemory)] = "1Gi"
		sc := &vdb.Spec.Subclusters[0]
		sc.Resources = v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("16"),
				v1.ResourceMemory: resource.MustParse("32Gi"),
			},
			Limits: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("32"),
				v1.ResourceMemory: resource.MustParse("64Gi"),
			},
		}
		nma := makeNMAContainer(vdb, sc)
		actual, _ := nma.Resources.Limits.Cpu().AsInt64()
		Ω(actual).Should(Equal(int64(8)))
		actual, _ = nma.Resources.Requests.Cpu().AsInt64()
		defQuantity := vmeta.DefaultNMAResources[v1.ResourceRequestsCPU]
		defVal, _ := defQuantity.AsInt64()
		Ω(actual).Should(Equal(defVal))
		actual, _ = nma.Resources.Limits.Memory().AsInt64()
		Ω(actual).Should(Equal(int64(1 * 1024 * 1024 * 1024)))
		actual, _ = nma.Resources.Requests.Memory().AsInt64()
		defQuantity = vmeta.DefaultNMAResources[v1.ResourceRequestsMemory]
		defVal, _ = defQuantity.AsInt64()
		Ω(actual).Should(Equal(defVal))
	})

	It("should omit NMA resources if annotation is set without a value", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		vdb.Annotations[vmeta.GenNMAResourcesAnnotationName(v1.ResourceLimitsCPU)] = ""
		vdb.Annotations[vmeta.GenNMAResourcesAnnotationName(v1.ResourceLimitsMemory)] = ""
		sc := &vdb.Spec.Subclusters[0]
		sc.Resources = v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("16"),
				v1.ResourceMemory: resource.MustParse("32Gi"),
			},
			Limits: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("32"),
				v1.ResourceMemory: resource.MustParse("64Gi"),
			},
		}
		nma := makeNMAContainer(vdb, sc)
		_, ok := nma.Resources.Limits[v1.ResourceCPU]
		Ω(ok).Should(BeFalse())
		_, ok = nma.Resources.Limits[v1.ResourceMemory]
		Ω(ok).Should(BeFalse())
		_, ok = nma.Resources.Requests[v1.ResourceCPU]
		Ω(ok).Should(BeTrue())
		_, ok = nma.Resources.Requests[v1.ResourceCPU]
		Ω(ok).Should(BeTrue())
		actual, _ := nma.Resources.Requests.Cpu().AsInt64()
		defQuantity := vmeta.DefaultNMAResources[v1.ResourceRequestsCPU]
		defVal, _ := defQuantity.AsInt64()
		Ω(actual).Should(Equal(defVal))
		actual, _ = nma.Resources.Requests.Memory().AsInt64()
		defQuantity = vmeta.DefaultNMAResources[v1.ResourceRequestsMemory]
		defVal, _ = defQuantity.AsInt64()
		Ω(actual).Should(Equal(defVal))
	})

	It("should allow health probe field to be overridden", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		sc := &vdb.Spec.Subclusters[0]
		vdb.Annotations[vmeta.GenNMAHealthProbeAnnotationName(vmeta.NMAHealthProbeStartup, vmeta.NMAHealthProbeSuccessThreshold)] = "13"
		vdb.Annotations[vmeta.GenNMAHealthProbeAnnotationName(vmeta.NMAHealthProbeLiveness, vmeta.NMAHealthProbeFailureThreshold)] = "8"
		nma := makeNMAContainer(vdb, sc)
		Ω(nma.StartupProbe.SuccessThreshold).Should(Equal(int32(13)))
		Ω(nma.ReadinessProbe.SuccessThreshold).Should(Equal(int32(0)))
		Ω(nma.LivenessProbe.FailureThreshold).Should(Equal(int32(8)))
	})
})

func getFirstSSHSecretVolumeMountIndex(c *v1.Container) (int, bool) {
	for i := range c.VolumeMounts {
		if c.VolumeMounts[i].Name == vapi.SSHMountName {
			return i, true
		}
	}
	return 0, false
}

// makeSubPaths is a helper that extracts all of the subPaths from the volume mounts.
func makeSubPaths(c *v1.Container) []string {
	sp := []string{}
	for i := range c.VolumeMounts {
		sp = append(sp, c.VolumeMounts[i].SubPath)
	}
	return sp
}

// makeVolumeNames is a helper that extracts all of the volume mount names from the volume mounts.
func makeVolumeMountNames(c *v1.Container) []string {
	volNames := []string{}
	for i := range c.VolumeMounts {
		volNames = append(volNames, c.VolumeMounts[i].Name)
	}
	return volNames
}

func getPodInfoVolume(vols []v1.Volume) *v1.Volume {
	return getVolume(vols, vapi.PodInfoMountName)
}

func getStartupConfVolume(vols []v1.Volume) *v1.Volume {
	return getVolume(vols, startupConfMountName)
}

func getVolume(vols []v1.Volume, mountName string) *v1.Volume {
	for i := range vols {
		if vols[i].Name == mountName {
			return &vols[i]
		}
	}
	return nil
}

func NMACertsVolumeExists(vdb *vapi.VerticaDB, vols []v1.Volume) bool {
	for i := range vols {
		if vols[i].Name == vapi.NMACertsMountName && vols[i].Secret.SecretName == vdb.Spec.NMATLSSecret {
			return true
		}
	}
	return false
}

func NMACertsVolumeMountExists(c *v1.Container) bool {
	for _, vol := range c.VolumeMounts {
		if vol.Name == vapi.NMACertsMountName && vol.MountPath == paths.NMACertsRoot {
			return true
		}
	}
	return false
}

func NMACertsEnvVarsExist(vdb *vapi.VerticaDB, c *v1.Container) bool {
	envMap := make(map[string]v1.EnvVar)
	for _, envVar := range c.Env {
		envMap[envVar.Name] = envVar
	}
	_, rootCAOk := envMap[NMARootCAEnv]
	_, certOk := envMap[NMACertEnv]
	_, keyOk := envMap[NMAKeyEnv]
	_, secretNamespaceOk := envMap[NMASecretNamespaceEnv]
	_, secretNameOk := envMap[NMASecretNameEnv]
	if vmeta.UseNMACertsMount(vdb.Annotations) {
		if rootCAOk && certOk && keyOk && !secretNamespaceOk && !secretNameOk {
			return true
		}
	} else {
		if !rootCAOk && !certOk && !keyOk && secretNamespaceOk && secretNameOk {
			return true
		}
	}
	return false
}

func isPasswdIncludedInPodInfo(vdb *vapi.VerticaDB, podSpec *v1.PodSpec) bool {
	v := getPodInfoVolume(podSpec.Volumes)
	for i := range v.Projected.Sources {
		if v.Projected.Sources[i].Secret != nil {
			if v.Projected.Sources[i].Secret.LocalObjectReference.Name == vdb.Spec.PasswordSecret {
				return true
			}
		}
	}
	return false
}
