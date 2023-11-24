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
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	v1 "k8s.io/api/core/v1"
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
			Expect(reflect.DeepEqual(c, baseContainer)).Should(BeTrue())
		}
	})

	It("should add our own capabilities to the securityContext for admintools only", func() {
		vdb := vapi.MakeVDB()
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(baseContainer.SecurityContext).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities.Add).Should(ContainElements([]v1.Capability{"SYS_CHROOT", "AUDIT_WRITE"}))

		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Spec.SecurityContext = &v1.SecurityContext{
			Capabilities: &v1.Capabilities{},
		}
		baseContainer = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(baseContainer.SecurityContext).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities.Add).ShouldNot(ContainElement([]v1.Capability{"SYS_CHROOT"}))
		Expect(baseContainer.SecurityContext.Capabilities.Add).ShouldNot(ContainElement([]v1.Capability{"AUDIT_WRITE"}))

	})

	It("should add omit our own capabilities in the securityContext if we are dropping them", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.SecurityContext = &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Drop: []v1.Capability{"AUDIT_WRITE"},
			},
		}
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(baseContainer.SecurityContext).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities.Add).Should(ContainElements([]v1.Capability{"SYS_CHROOT"}))
		Expect(baseContainer.SecurityContext.Capabilities.Add).ShouldNot(ContainElement([]v1.Capability{"AUDIT_WRITE"}))
	})

	It("should allow you to run in priv mode", func() {
		vdb := vapi.MakeVDB()
		priv := true
		vdb.Spec.SecurityContext = &v1.SecurityContext{
			Privileged: &priv,
		}
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(baseContainer.SecurityContext).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Privileged).ShouldNot(BeNil())
		Expect(*baseContainer.SecurityContext.Privileged).Should(BeTrue())
	})

	It("should add a catalog mount point if it differs from data", func() {
		vdb := vapi.MakeVDB()
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("catalog")))
		vdb.Spec.Local.CatalogPath = "/catalog"
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("catalog")))
	})

	It("should only have separate mount paths for data, depot and catalog if they are different", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Local.DataPath = "/vertica"
		vdb.Spec.Local.DepotPath = vdb.Spec.Local.DataPath
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("catalog")))
		Expect(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("depot")))
		Expect(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("data")))
		vdb.Spec.Local.DepotPath = "/depot"
		vdb.Spec.Local.CatalogPath = vdb.Spec.Local.DepotPath
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("depot")))
		Expect(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("catalog")))
		Expect(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("data")))
		vdb.Spec.Local.CatalogPath = "/vertica/catalog"
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("catalog")))
	})

	It("should have a specific mount name and no subPath for depot if depotVolume is EmptyDir", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Local.DepotVolume = vapi.PersistentVolume
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(makeVolumeMountNames(&c)).ShouldNot(ContainElement(ContainSubstring(vapi.DepotMountName)))
		Expect(makeSubPaths(&c)).Should(ContainElement(ContainSubstring("depot")))
		vdb.Spec.Local.DepotVolume = vapi.EmptyDir
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(makeVolumeMountNames(&c)).Should(ContainElement(ContainSubstring(vapi.DepotMountName)))
		Expect(makeSubPaths(&c)).ShouldNot(ContainElement(ContainSubstring("depot")))
	})

	It("shoul have all probes use the htpp version endpoint", func() {
		vdb := vapi.MakeVDB()

		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(c.ReadinessProbe.HTTPGet.Path).Should(Equal(HTTPServerVersionPath))
		Expect(c.ReadinessProbe.HTTPGet.Port).Should(Equal(intstr.FromInt(VerticaHTTPPort)))
		Expect(c.ReadinessProbe.HTTPGet.Scheme).Should(Equal(v1.URISchemeHTTPS))
		Expect(c.LivenessProbe.HTTPGet.Path).Should(Equal(HTTPServerVersionPath))
		Expect(c.LivenessProbe.HTTPGet.Port).Should(Equal(intstr.FromInt(VerticaHTTPPort)))
		Expect(c.LivenessProbe.HTTPGet.Scheme).Should(Equal(v1.URISchemeHTTPS))
		Expect(c.StartupProbe.HTTPGet.Path).Should(Equal(HTTPServerVersionPath))
		Expect(c.StartupProbe.HTTPGet.Port).Should(Equal(intstr.FromInt(VerticaHTTPPort)))
		Expect(c.StartupProbe.HTTPGet.Scheme).Should(Equal(v1.URISchemeHTTPS))
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
		Expect(c.ReadinessProbe.Exec.Command).Should(Equal(NewCommand))
		Expect(c.ReadinessProbe.TimeoutSeconds).Should(Equal(NewTimeout))
		Expect(c.ReadinessProbe.FailureThreshold).Should(Equal(NewFailureThreshold))
		Expect(c.ReadinessProbe.InitialDelaySeconds).Should(Equal(NewInitialDelaySeconds))
		Expect(c.ReadinessProbe.PeriodSeconds).Should(Equal(NewPeriodSeconds))
		Expect(c.ReadinessProbe.SuccessThreshold).Should(Equal(NewSuccessThreshold))
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
		Expect(c.LivenessProbe.TimeoutSeconds).Should(Equal(NewTimeout))
		Expect(c.StartupProbe.PeriodSeconds).Should(Equal(NewPeriodSeconds))
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
		Expect(c.Containers[0].ReadinessProbe.Exec).Should(BeNil())
		Expect(c.Containers[0].ReadinessProbe.GRPC).ShouldNot(BeNil())
		Expect(c.Containers[0].LivenessProbe.Exec).Should(BeNil())
		Expect(c.Containers[0].LivenessProbe.HTTPGet).ShouldNot(BeNil())
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
		Expect(len(c.SecurityContext.Sysctls)).Should(Equal(2))
		Expect(c.SecurityContext.Sysctls[0].Name).Should(Equal("net.ipv4.tcp_keepalive_time"))
		Expect(c.SecurityContext.Sysctls[0].Value).Should(Equal("45"))
		Expect(c.SecurityContext.Sysctls[1].Name).Should(Equal("net.ipv4.tcp_keepalive_intvl"))
		Expect(c.SecurityContext.Sysctls[1].Value).Should(Equal("5"))
	})

	It("should mount ssh secret for dbadmin and root", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.SSHSecAnnotation] = "my-secret"
		c := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		cnt := &c.Containers[0]
		i, ok := getFirstSSHSecretVolumeMountIndex(cnt)
		Expect(ok).Should(BeTrue())
		const ExpectedPathsPerMount = 3
		Expect(len(cnt.VolumeMounts)).Should(BeNumerically(">=", i+2*ExpectedPathsPerMount))
		for j := 0; i < ExpectedPathsPerMount; i++ {
			Expect(cnt.VolumeMounts[i+j].MountPath).Should(ContainSubstring(paths.DBAdminSSHPath))
		}
		for j := 0; i < ExpectedPathsPerMount; i++ {
			Expect(cnt.VolumeMounts[i+ExpectedPathsPerMount+j].MountPath).Should(ContainSubstring(paths.RootSSHPath))
		}
	})

	It("should mount or not mount NMA certs volume according to annotation", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Annotations[vmeta.MountNMACerts] = vmeta.MountNMACertsFalse
		ps := buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeFalse())
		Expect(NMACertsVolumeMountExists(&c)).Should(BeFalse())
		Expect(NMACertsEnvVarsExist(vdb, &c)).Should(BeTrue())
		vdb.Annotations[vmeta.MountNMACerts] = vmeta.MountNMACertsTrue
		ps = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeTrue())
		Expect(NMACertsVolumeMountExists(&c)).Should(BeTrue())
		Expect(NMACertsEnvVarsExist(vdb, &c)).Should(BeTrue())
		// test default value (which should be true)
		delete(vdb.Annotations, vmeta.MountNMACerts)
		ps = buildPodSpec(vdb, &vdb.Spec.Subclusters[0])
		c = makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(NMACertsVolumeExists(vdb, ps.Volumes)).Should(BeTrue())
		Expect(NMACertsVolumeMountExists(&c)).Should(BeTrue())
		Expect(NMACertsEnvVarsExist(vdb, &c)).Should(BeTrue())
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
