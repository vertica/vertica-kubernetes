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
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	SuperuserPasswordPath   = "superuser-passwd"
	TestStorageClassName    = "test-storage-class"
	VerticaClientPort       = 5433
	VerticaAgentPort        = 5444
	VerticaHTTPPort         = 8443
	InternalVerticaCommPort = 5434
	SSHPort                 = 22
	VerticaClusterCommPort  = 5434
	SpreadClientPort        = 4803

	// Standard environment variables that are set in each pod
	PodIPEnv        = "POD_IP"
	HostIPEnv       = "HOST_IP"
	HostNameEnv     = "HOST_NODENAME"
	DataPathEnv     = "DATA_PATH"
	CatalogPathEnv  = "CATALOG_PATH"
	DepotPathEnv    = "DEPOT_PATH"
	DatabaseNameEnv = "DATABASE_NAME"
)

// BuildExtSvc creates desired spec for the external service.
func BuildExtSvc(nm types.NamespacedName, vdb *vapi.VerticaDB, sc *vapi.Subcluster,
	selectorLabelCreator func(*vapi.VerticaDB, *vapi.Subcluster) map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      MakeLabelsForSvcObject(vdb, sc, "external"),
			Annotations: MakeAnnotationsForSubclusterService(vdb, sc),
		},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabelCreator(vdb, sc),
			Type:     sc.ServiceType,
			Ports: []corev1.ServicePort{
				{Port: VerticaClientPort, Name: "vertica", NodePort: sc.NodePort},
				{Port: VerticaHTTPPort, Name: "vertica-http", NodePort: sc.VerticaHTTPNodePort},
				{Port: VerticaAgentPort, Name: "agent"},
			},
			ExternalIPs:    sc.ExternalIPs,
			LoadBalancerIP: sc.LoadBalancerIP,
		},
	}
}

// BuildHlSvc creates the desired spec for the headless service.
func BuildHlSvc(nm types.NamespacedName, vdb *vapi.VerticaDB) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      MakeLabelsForSvcObject(vdb, nil, "headless"),
			Annotations: MakeAnnotationsForObject(vdb),
		},
		Spec: corev1.ServiceSpec{
			Selector:                 MakeBaseSvcSelectorLabels(vdb),
			ClusterIP:                "None",
			Type:                     "ClusterIP",
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{Port: SSHPort, Name: "tcp-ssh"},
				{Port: VerticaClusterCommPort, Name: "tcp-verticaclustercomm"},
				{Port: SpreadClientPort, Name: "tcp-spreadclient"},
			},
		},
	}
}

// buildVolumeMounts returns the volume mounts to include in the sts pod spec
func buildVolumeMounts(vdb *vapi.VerticaDB) []corev1.VolumeMount {
	volMnts := []corev1.VolumeMount{
		{Name: vapi.LocalDataPVC, MountPath: paths.LocalDataPath},
		{Name: vapi.LocalDataPVC, SubPath: vdb.GetPVSubPath("config"), MountPath: paths.ConfigPath},
		{Name: vapi.LocalDataPVC, SubPath: vdb.GetPVSubPath("log"), MountPath: paths.LogPath},
		{Name: vapi.LocalDataPVC, SubPath: vdb.GetPVSubPath("data"), MountPath: vdb.Spec.Local.DataPath},
		{Name: vapi.PodInfoMountName, MountPath: paths.PodInfoPath},
	}
	// Only mount separate depot/catalog paths if the paths are different in the
	// container. Otherwise, you will get multiple mount points shared the same
	// path, which will prevent any pods from starting.
	if vdb.Spec.Local.DataPath != vdb.Spec.Local.DepotPath {
		if vdb.IsDepotVolumeEmptyDir() {
			// If depotVolume is EmptyDir, the depot is stored in its own 'emptyDir' volume
			volMnts = append(volMnts, corev1.VolumeMount{
				Name: vapi.DepotMountName, MountPath: vdb.Spec.Local.DepotPath,
			})
		} else {
			volMnts = append(volMnts, corev1.VolumeMount{
				Name: vapi.LocalDataPVC, SubPath: vdb.GetPVSubPath("depot"), MountPath: vdb.Spec.Local.DepotPath,
			})
		}
	}
	if vdb.Spec.Local.GetCatalogPath() != vdb.Spec.Local.DataPath && vdb.Spec.Local.GetCatalogPath() != vdb.Spec.Local.DepotPath {
		volMnts = append(volMnts, corev1.VolumeMount{
			Name: vapi.LocalDataPVC, SubPath: vdb.GetPVSubPath("catalog"), MountPath: vdb.Spec.Local.GetCatalogPath(),
		})
	}

	if vdb.Spec.LicenseSecret != "" {
		volMnts = append(volMnts, corev1.VolumeMount{
			Name:      vapi.LicensingMountName,
			MountPath: paths.MountedLicensePath,
		})
	}

	if vdb.Spec.Communal.HadoopConfig != "" {
		volMnts = append(volMnts, corev1.VolumeMount{
			Name:      vapi.HadoopConfigMountName,
			MountPath: paths.HadoopConfPath,
		})
	}

	if vdb.Spec.KerberosSecret != "" {
		volMnts = append(volMnts, buildKerberosVolumeMounts()...)
	}

	if vdb.Spec.SSHSecret != "" {
		volMnts = append(volMnts, buildSSHVolumeMounts()...)
	}

	if vdb.Spec.HTTPServerTLSSecret != "" {
		volMnts = append(volMnts, buildHTTPServerVolumeMount()...)
	}

	volMnts = append(volMnts, buildCertSecretVolumeMounts(vdb)...)
	volMnts = append(volMnts, vdb.Spec.VolumeMounts...)

	return volMnts
}

func buildKerberosVolumeMounts() []corev1.VolumeMount {
	// We create two mounts.  One is to set /etc/krb5.conf.  It needs to be set
	// at the specific location.  The second one is to mount a directory that
	// contains all of the keys in the Kerberos secret.  We mount the entire
	// directory, as opposed to using SubPath, so that the keytab file within
	// the Secret will automatically get updated if the Secret is updated.  This
	// saves having to restart the pod if the keytab changes.
	return []corev1.VolumeMount{
		{
			Name:      vapi.Krb5SecretMountName,
			MountPath: paths.Krb5Conf,
			SubPath:   filepath.Base(paths.Krb5Conf),
		},
		{
			Name:      vapi.Krb5SecretMountName,
			MountPath: filepath.Dir(paths.Krb5Keytab),
		},
	}
}

func buildSSHVolumeMounts() []corev1.VolumeMount {
	mnts := []corev1.VolumeMount{}
	for _, p := range paths.SSHKeyPaths {
		mnts = append(mnts, corev1.VolumeMount{
			Name:      vapi.SSHMountName,
			MountPath: fmt.Sprintf("%s/%s", paths.SSHPath, p),
			SubPath:   p,
		})
	}
	return mnts
}

func buildHTTPServerVolumeMount() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      vapi.HTTPServerCertsMountName,
			MountPath: paths.HTTPServerCertsRoot,
		},
	}
}

// buildCertSecretVolumeMounts returns the volume mounts for any cert secrets that are in the vdb
func buildCertSecretVolumeMounts(vdb *vapi.VerticaDB) []corev1.VolumeMount {
	mnts := []corev1.VolumeMount{}
	for _, s := range vdb.Spec.CertSecrets {
		mnts = append(mnts, corev1.VolumeMount{
			Name:      s.Name,
			MountPath: fmt.Sprintf("%s/%s", paths.CertsRoot, s.Name),
		})
	}
	return mnts
}

// buildVolumes builds up a list of volumes to include in the sts
func buildVolumes(vdb *vapi.VerticaDB, deployNames *DeploymentNames) []corev1.Volume {
	vols := []corev1.Volume{}
	vols = append(vols, buildPodInfoVolume(vdb, deployNames))
	if vdb.Spec.LicenseSecret != "" {
		vols = append(vols, buildLicenseVolume(vdb))
	}
	if vdb.Spec.Communal.HadoopConfig != "" {
		vols = append(vols, buildHadoopConfigVolume(vdb))
	}
	if vdb.Spec.KerberosSecret != "" {
		vols = append(vols, buildKerberosVolume(vdb))
	}
	if vdb.Spec.SSHSecret != "" {
		vols = append(vols, buildSSHVolume(vdb))
	}
	if vdb.Spec.HTTPServerTLSSecret != "" {
		vols = append(vols, buildHTTPServerSecretVolume(vdb))
	}
	if vdb.IsDepotVolumeEmptyDir() {
		vols = append(vols, buildDepotVolume())
	}
	vols = append(vols, buildCertSecretVolumes(vdb)...)
	vols = append(vols, vdb.Spec.Volumes...)
	return vols
}

// buildLicenseVolume returns a volume that contains any licenses
func buildLicenseVolume(vdb *vapi.VerticaDB) corev1.Volume {
	return corev1.Volume{
		Name: vapi.LicensingMountName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: vdb.Spec.LicenseSecret,
			},
		},
	}
}

// buildPodInfoVolume constructs the volume that has the /etc/podinfo files.
func buildPodInfoVolume(vdb *vapi.VerticaDB, deployNames *DeploymentNames) corev1.Volume {
	projSources := []corev1.VolumeProjection{
		{DownwardAPI: buildDownwardAPIProjection()},
		{ConfigMap: buildOperatorConfigMapProjection(deployNames)},
		// If these is a superuser password, include that in the projection
		{Secret: buildSuperuserPasswordProjection(vdb)},
	}

	return corev1.Volume{
		Name: vapi.PodInfoMountName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: projSources,
			},
		},
	}
}

// buildDownwardAPIProjection creates a projection from the downwardAPI for
// inclusion in /etc/podinfo
func buildDownwardAPIProjection() *corev1.DownwardAPIProjection {
	return &corev1.DownwardAPIProjection{
		Items: []corev1.DownwardAPIVolumeFile{
			{
				Path: "memory-limit",
				ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource:      "limits.memory",
					ContainerName: names.ServerContainer,
				},
			},
			{
				Path: "memory-request",
				ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource:      "requests.memory",
					ContainerName: names.ServerContainer,
				},
			},
			{
				Path: "cpu-limit",
				ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource:      "limits.cpu",
					ContainerName: names.ServerContainer,
				},
			},
			{
				Path: "cpu-request",
				ResourceFieldRef: &corev1.ResourceFieldSelector{
					Resource:      "requests.cpu",
					ContainerName: names.ServerContainer,
				},
			},
			{
				Path: "labels",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.labels",
				},
			},
			{
				Path: "annotations",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.annotations",
				},
			},
			{
				Path: "name",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
			{
				Path: "namespace",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
			{
				Path: "k8s-version",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.annotations['%s']", vmeta.KubernetesVersionAnnotation),
				},
			},
			{
				Path: "k8s-git-commit",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.annotations['%s']", vmeta.KubernetesGitCommitAnnotation),
				},
			},
			{
				Path: "k8s-build-date",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.annotations['%s']", vmeta.KubernetesBuildDateAnnotation),
				},
			},
		},
	}
}

// buildOperatorConfigMapProjection creates a projection for inclusion in /etc/podinfo
func buildOperatorConfigMapProjection(deployNames *DeploymentNames) *corev1.ConfigMapProjection {
	return &corev1.ConfigMapProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: deployNames.getConfigMapName()},
		Items: []corev1.KeyToPath{
			{Key: "DEPLOY_WITH", Path: "operator-deployment-method"},
			{Key: "VERSION", Path: "operator-version"},
		},
	}
}

// probeContainsSuperuserPassword will check if the probe uses the superuser
// password.
func probeContainsSuperuserPassword(probe *corev1.Probe) bool {
	if probe.Exec == nil {
		return false
	}
	for _, v := range probe.Exec.Command {
		if strings.Contains(v, SuperuserPasswordPath) {
			return true
		}
	}
	return false
}

// requiresSuperuserPasswordSecretMount returns true if the superuser password
// needs to be mounted in the pod.
func requiresSuperuserPasswordSecretMount(vdb *vapi.VerticaDB) bool {
	if vdb.Spec.SuperuserPasswordSecret == "" {
		return false
	}

	// Construct each probe. If don't use the superuser password in them, then
	// it is safe to not mount this in the downward API projection.
	funcs := []func(*vapi.VerticaDB) *corev1.Probe{
		makeReadinessProbe, makeStartupProbe, makeLivenessProbe,
	}
	for _, f := range funcs {
		if probeContainsSuperuserPassword(f(vdb)) {
			return true
		}
	}
	return false
}

// buildSuperuserPasswordProjection creates a projection for inclusion in /etc/podinfo
func buildSuperuserPasswordProjection(vdb *vapi.VerticaDB) *corev1.SecretProjection {
	if requiresSuperuserPasswordSecretMount(vdb) {
		return &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: vdb.Spec.SuperuserPasswordSecret},
			Items: []corev1.KeyToPath{
				{Key: SuperuserPasswordKey, Path: SuperuserPasswordPath},
			},
		}
	}
	return nil
}

// buildCertSecretVolumes returns a list of volumes, one for each secret in certSecrets.
func buildCertSecretVolumes(vdb *vapi.VerticaDB) []corev1.Volume {
	vols := []corev1.Volume{}
	for _, s := range vdb.Spec.CertSecrets {
		vols = append(vols, corev1.Volume{
			Name: s.Name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: s.Name},
			},
		})
	}
	return vols
}

func buildHadoopConfigVolume(vdb *vapi.VerticaDB) corev1.Volume {
	return corev1.Volume{
		Name: vapi.HadoopConfigMountName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: vdb.Spec.Communal.HadoopConfig},
			},
		},
	}
}

func buildKerberosVolume(vdb *vapi.VerticaDB) corev1.Volume {
	return corev1.Volume{
		Name: vapi.Krb5SecretMountName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: vdb.Spec.KerberosSecret,
			},
		},
	}
}

func buildSSHVolume(vdb *vapi.VerticaDB) corev1.Volume {
	return corev1.Volume{
		Name: vapi.SSHMountName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: vdb.Spec.SSHSecret,
			},
		},
	}
}

func buildHTTPServerSecretVolume(vdb *vapi.VerticaDB) corev1.Volume {
	return corev1.Volume{
		Name: vapi.HTTPServerCertsMountName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: vdb.Spec.HTTPServerTLSSecret,
			},
		},
	}
}

// buildEmptyDirVolume returns a generic 'emptyDir' volume
func buildEmptyDirVolume(volName string) corev1.Volume {
	return corev1.Volume{
		Name: volName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// buildDepotVolume returns an 'emptyDir' volume for the depot
func buildDepotVolume() corev1.Volume {
	return buildEmptyDirVolume(vapi.DepotMountName)
}

// buildPodSpec creates a PodSpec for the statefulset
func buildPodSpec(vdb *vapi.VerticaDB, sc *vapi.Subcluster, deployNames *DeploymentNames) corev1.PodSpec {
	termGracePeriod := int64(0)
	return corev1.PodSpec{
		NodeSelector:                  sc.NodeSelector,
		Affinity:                      GetK8sAffinity(sc.Affinity),
		Tolerations:                   sc.Tolerations,
		ImagePullSecrets:              GetK8sLocalObjectReferenceArray(vdb.Spec.ImagePullSecrets),
		Containers:                    makeContainers(vdb, sc),
		Volumes:                       buildVolumes(vdb, deployNames),
		TerminationGracePeriodSeconds: &termGracePeriod,
		ServiceAccountName:            deployNames.ServiceAccountName,
		SecurityContext:               buildPodSecurityPolicy(vdb),
	}
}

// buildPodSecurityPolicy will create the security policy for the pod spec
func buildPodSecurityPolicy(vdb *vapi.VerticaDB) *corev1.PodSecurityContext {
	// If anything was specified in the vdb, we use that as the base. Otherwise,
	// we just use an empty context.
	psc := corev1.PodSecurityContext{}
	if vdb.Spec.PodSecurityContext != nil {
		vdb.Spec.PodSecurityContext.DeepCopyInto(&psc)
	}
	if psc.FSGroup == nil {
		// Set the FSGroup so that mounted volumes have the dbadmin gid. This gives
		// pods write access to the volumes. Note in 1.9.0 and prior versions of the
		// operator we did not have this and instead relied on the vertica image to
		// set the required permissions via chmod.
		const DefaultDbadminGID = 5000
		dbadminGID := int64(DefaultDbadminGID)
		psc.FSGroup = &dbadminGID
	}
	return &psc
}

// makeServerContainer builds the spec for the server container
func makeServerContainer(vdb *vapi.VerticaDB, sc *vapi.Subcluster) corev1.Container {
	envVars := translateAnnotationsToEnvVars(vdb)
	envVars = append(envVars, []corev1.EnvVar{
		{Name: PodIPEnv, ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"}},
		},
		{Name: HostIPEnv, ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}},
		},
		{Name: HostNameEnv, ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"}},
		},
		{Name: DataPathEnv, Value: vdb.Spec.Local.DataPath},
		{Name: DepotPathEnv, Value: vdb.Spec.Local.DepotPath},
		{Name: CatalogPathEnv, Value: vdb.Spec.Local.GetCatalogPath()},
		{Name: DatabaseNameEnv, Value: vdb.Spec.DBName},
	}...)
	return corev1.Container{
		Image:           pickImage(vdb, sc),
		ImagePullPolicy: vdb.Spec.ImagePullPolicy,
		Name:            names.ServerContainer,
		Resources:       sc.Resources,
		Ports: []corev1.ContainerPort{
			{ContainerPort: VerticaClientPort, Name: "vertica"},
			{ContainerPort: InternalVerticaCommPort, Name: "vertica-int"},
			{ContainerPort: SSHPort, Name: "ssh"},
			{ContainerPort: VerticaAgentPort, Name: "agent"},
		},
		ReadinessProbe:  makeReadinessProbe(vdb),
		LivenessProbe:   makeLivenessProbe(vdb),
		StartupProbe:    makeStartupProbe(vdb),
		SecurityContext: makeServerSecurityContext(vdb),
		Env:             envVars,
		VolumeMounts:    buildVolumeMounts(vdb),
	}
}

// makeReadinessProbe will build the readiness probe. It has a default probe
// that can be overridden with the spec.readinessProbeOverride parameter.
func makeReadinessProbe(vdb *vapi.VerticaDB) *corev1.Probe {
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"bash", "-c", buildCanaryQuerySQL(vdb)},
			},
		},
	}
	overrideProbe(probe, vdb.Spec.ReadinessProbeOverride)
	return probe
}

// makeStartupProbe will return the Probe object to use for the startup probe.
func makeStartupProbe(vdb *vapi.VerticaDB) *corev1.Probe {
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"bash", "-c", buildCanaryQuerySQL(vdb)},
			},
		},
		// We want to wait about 20 minutes for the server to come up before the
		// other probes come into affect. The total length of the probe is more or
		// less: InitialDelaySeconds + PeriodSeconds * FailureThreshold.
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		FailureThreshold:    117,
		TimeoutSeconds:      5,
	}
	overrideProbe(probe, vdb.Spec.StartupProbeOverride)
	return probe
}

// makeLivenessProbe will return the Probe object to use for the liveness probe.
func makeLivenessProbe(vdb *vapi.VerticaDB) *corev1.Probe {
	probe := &corev1.Probe{
		// We check if the TCP client port is open. We used this approach,
		// rather than issuing 'select 1' like readinessProbe because we need
		// to minimize variability. If the livenessProbe fails, the pod is
		// rescheduled. So, it isn't as forgiving as the readinessProbe.
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(VerticaClientPort),
			},
		},
		// These values were picked so that we can estimate how long vertica
		// needs to be unresponsive before it gets killed. We are targeting
		// about 2.5 minutes after initial start and 1.5 minutes if the pod has
		// been running for a while. The formula is:
		// InitialDelaySeconds + PeriodSeconds * FailureThreshold.
		//
		// Note: InitialDelaySeconds only applies the first time after pod
		// scheduling.
		InitialDelaySeconds: 60,
		TimeoutSeconds:      1,
		PeriodSeconds:       30,
		FailureThreshold:    3,
	}
	overrideProbe(probe, vdb.Spec.LivenessProbeOverride)
	return probe
}

// overrideProbe will modify the probe with any user defined override values.
func overrideProbe(probe, ov *corev1.Probe) {
	if ov == nil {
		return
	}
	// Merge in parts of the override into the default probe
	//
	// You can only set one handler (exec, tcpSocket, httpGet or grpc). If the
	// override has any one of those set, we always clear the other ones. A
	// webhook exists that prevents setting more than one in the CR.
	if ov.Exec != nil {
		probe.Exec = ov.Exec
		probe.TCPSocket = nil
		probe.HTTPGet = nil
		probe.GRPC = nil
	}
	if ov.TCPSocket != nil {
		probe.Exec = nil
		probe.TCPSocket = ov.TCPSocket
		probe.HTTPGet = nil
		probe.GRPC = nil
	}
	if ov.HTTPGet != nil {
		probe.Exec = nil
		probe.TCPSocket = nil
		probe.HTTPGet = ov.HTTPGet
		probe.GRPC = nil
	}
	if ov.GRPC != nil {
		probe.Exec = nil
		probe.TCPSocket = nil
		probe.HTTPGet = nil
		probe.GRPC = ov.GRPC
	}
	if ov.FailureThreshold > 0 {
		probe.FailureThreshold = ov.FailureThreshold
	}
	if ov.InitialDelaySeconds > 0 {
		probe.InitialDelaySeconds = ov.InitialDelaySeconds
	}
	if ov.PeriodSeconds > 0 {
		probe.PeriodSeconds = ov.PeriodSeconds
	}
	if ov.SuccessThreshold > 0 {
		probe.SuccessThreshold = ov.SuccessThreshold
	}
	if ov.TimeoutSeconds > 0 {
		probe.TimeoutSeconds = ov.TimeoutSeconds
	}
}

func makeServerSecurityContext(vdb *vapi.VerticaDB) *corev1.SecurityContext {
	sc := &corev1.SecurityContext{}
	if vdb.Spec.SecurityContext != nil {
		sc = vdb.Spec.SecurityContext
	}
	if sc.Capabilities == nil {
		sc.Capabilities = &corev1.Capabilities{}
	}
	capabilitiesNeeded := []corev1.Capability{
		// Needed to run sshd on OpenShift
		"SYS_CHROOT",
		// Needed to run sshd on OpenShift
		"AUDIT_WRITE",
		// Needed to be able to collect stacks via vstack
		"SYS_PTRACE",
	}
	for i := range capabilitiesNeeded {
		foundCap := false
		for j := range sc.Capabilities.Add {
			if capabilitiesNeeded[i] == sc.Capabilities.Add[j] {
				foundCap = true
				break
			}
		}
		for j := range sc.Capabilities.Drop {
			if capabilitiesNeeded[i] == sc.Capabilities.Drop[j] {
				// If the capability we want to add is *dropped*, we won't bother adding it in
				foundCap = false
				break
			}
		}
		if !foundCap {
			sc.Capabilities.Add = append(sc.Capabilities.Add, capabilitiesNeeded[i])
		}
	}
	return sc
}

// makeContainers creates the list of containers to include in the pod spec.
func makeContainers(vdb *vapi.VerticaDB, sc *vapi.Subcluster) []corev1.Container {
	cnts := []corev1.Container{makeServerContainer(vdb, sc)}
	for i := range vdb.Spec.Sidecars {
		c := vdb.Spec.Sidecars[i]
		// Append the standard volume mounts to the container.  This is done
		// because some of the the mount path include the UID, which isn't know
		// prior to the creation of the VerticaDB.
		c.VolumeMounts = append(c.VolumeMounts, buildVolumeMounts(vdb)...)
		// Append additional environment variables passed through annotations.
		c.Env = append(c.Env, translateAnnotationsToEnvVars(vdb)...)
		// As a convenience, add the catalog path as an environment variable.
		c.Env = append(c.Env, corev1.EnvVar{Name: "DBPATH", Value: vdb.GetDBCatalogPath()})
		cnts = append(cnts, c)
	}
	return cnts
}

// translateAnnotationsToEnvVars returns a list of EnvVars from the annotations
// in the CR
func translateAnnotationsToEnvVars(vdb *vapi.VerticaDB) []corev1.EnvVar {
	envVars := []corev1.EnvVar{}
	// regexp to match annotations starting with a letter
	m1 := regexp.MustCompile(`^[a-zA-Z].*`)
	// regexp to match any non-alphanumerical character
	m2 := regexp.MustCompile(`[^a-zA-Z0-9]`)
	for k, v := range vdb.Spec.Annotations {
		if !m1.MatchString(k) {
			continue
		}
		name := strings.ToUpper(m2.ReplaceAllString(k, "_"))
		envVars = append(envVars, corev1.EnvVar{
			Name:  name,
			Value: v,
		})
	}
	// We must always sort the list of envVars.  Failure to do this could cause
	// the statefulset controller to think the container that has the envVars
	// has changed.  But in reality, the containers are identical except for the
	// order of the vars.
	sort.Slice(envVars, func(i, j int) bool {
		return envVars[i].Name < envVars[j].Name
	})
	return envVars
}

// pickImage will pick the correct image for the subcluster to use
func pickImage(vdb *vapi.VerticaDB, sc *vapi.Subcluster) string {
	// The ImageOverride exists to allow standby subclusters created for
	// primaries to continue to use the old image during an online upgrade.
	if sc.ImageOverride != "" {
		return sc.ImageOverride
	}
	return vdb.Spec.Image
}

// getStorageClassName returns a  pointer to the StorageClass
func getStorageClassName(vdb *vapi.VerticaDB) *string {
	if vdb.Spec.Local.StorageClass == "" {
		return nil
	}
	return &vdb.Spec.Local.StorageClass
}

// BuildStsSpec builds manifest for a subclusters statefulset
func BuildStsSpec(nm types.NamespacedName, vdb *vapi.VerticaDB, sc *vapi.Subcluster, deployNames *DeploymentNames) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      makeLabelsForObject(vdb, sc, false),
			Annotations: MakeAnnotationsForObject(vdb),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: MakeStsSelectorLabels(vdb, sc),
			},
			ServiceName: names.GenHlSvcName(vdb).Name,
			Replicas:    &sc.Size,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      MakeLabelsForPodObject(vdb, sc),
					Annotations: MakeAnnotationsForObject(vdb),
				},
				Spec: buildPodSpec(vdb, sc, deployNames),
			},
			UpdateStrategy:      makeUpdateStrategy(vdb),
			PodManagementPolicy: appsv1.ParallelPodManagement,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: vapi.LocalDataPVC,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: getStorageClassName(vdb),
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: vdb.Spec.Local.RequestSize,
							},
						},
					},
				},
			},
		},
	}
}

// buildPod will construct a spec for a pod.
// This is only here for testing purposes when we need to construct the pods ourselves.  This
// bit is typically handled by the statefulset controller.
func BuildPod(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) *corev1.Pod {
	nm := names.GenPodName(vdb, sc, podIndex)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      MakeLabelsForPodObject(vdb, sc),
			Annotations: MakeAnnotationsForObject(vdb),
		},
		Spec: buildPodSpec(vdb, sc, DefaultDeploymentNames()),
	}
	// Setup default values for the DC table annotations.  These are normally
	// added by the AnnotationAndLabelPodReconciler.  However, this function is for test
	// purposes, and we have a few dependencies on these annotations.  Rather
	// than having many tests run the reconciler, we will add in sample values.
	pod.Annotations[vmeta.KubernetesBuildDateAnnotation] = "2022-03-16T15:58:47Z"
	pod.Annotations[vmeta.KubernetesGitCommitAnnotation] = "c285e781331a3785a7f436042c65c5641ce8a9e9"
	pod.Annotations[vmeta.KubernetesVersionAnnotation] = "v1.23.5"
	// Set a few things in the spec that are normally done by the statefulset
	// controller. Again, this is for testing purposes only as the statefulset
	// controller handles adding of the PVC to the volume list.
	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: vapi.LocalDataPVC,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: names.GenPVCName(vdb, sc, podIndex).Name,
			},
		},
	})
	pod.Spec.Hostname = nm.Name
	pod.Spec.Subdomain = names.GenHlSvcName(vdb).Name
	return pod
}

// BuildPVC will build a PVC for test purposes
func BuildPVC(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) *corev1.PersistentVolumeClaim {
	scn := TestStorageClassName
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.GenPVCName(vdb, sc, podIndex).Name,
			Namespace: vdb.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: vdb.Spec.Local.RequestSize,
				},
			},
			StorageClassName: &scn,
		},
	}
}

// BuildPV will build a PV for test purposes
func BuildPV(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) *corev1.PersistentVolume {
	hostPathType := corev1.HostPathDirectoryOrCreate
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: names.GenPVName(vdb, sc, podIndex).Name,
		},
		Spec: corev1.PersistentVolumeSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: vdb.Spec.Local.RequestSize,
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/host",
					Type: &hostPathType,
				},
			},
		},
	}
}

// BuildStorageClass will construct a storageClass for test purposes
func BuildStorageClass(allowVolumeExpansion bool) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: TestStorageClassName,
		},
		Provisioner:          "vertica.com/dummy-provisioner",
		AllowVolumeExpansion: &allowVolumeExpansion,
	}
}

// BuildS3CommunalCredSecret is a test helper to build up the Secret spec to store communal credentials
func BuildS3CommunalCredSecret(vdb *vapi.VerticaDB, accessKey, secretKey string) *corev1.Secret {
	nm := names.GenCommunalCredSecretName(vdb)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
		},
		Data: map[string][]byte{
			cloud.CommunalAccessKeyName: []byte(accessKey),
			cloud.CommunalSecretKeyName: []byte(secretKey),
		},
	}
	return secret
}

// BuildAzureAccountKeyCommunalCredSecret builds a secret that is setup for
// Azure using an account key.
func BuildAzureAccountKeyCommunalCredSecret(vdb *vapi.VerticaDB, accountName, accountKey string) *corev1.Secret {
	nm := names.GenCommunalCredSecretName(vdb)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
		},
		Data: map[string][]byte{
			cloud.AzureAccountName: []byte(accountName),
			cloud.AzureAccountKey:  []byte(accountKey),
		},
	}
	return secret
}

// BuildAzureSASCommunalCredSecret builds a secret that is setup for Azure using
// shared access signature.
func BuildAzureSASCommunalCredSecret(vdb *vapi.VerticaDB, blobEndpoint, sas string) *corev1.Secret {
	nm := names.GenCommunalCredSecretName(vdb)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
		},
		Data: map[string][]byte{
			cloud.AzureBlobEndpoint:          []byte(blobEndpoint),
			cloud.AzureSharedAccessSignature: []byte(sas),
		},
	}
	return secret
}

// BuildS3SseCustomerKeySecret is a test helper that builds a secret that is setup for
// S3 SSE-C server-side encryption
func BuildS3SseCustomerKeySecret(vdb *vapi.VerticaDB, clientKey string) *corev1.Secret {
	nm := names.GenS3SseCustomerKeySecretName(vdb)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
		},
		StringData: map[string]string{
			cloud.S3SseCustomerKeyName: clientKey,
		},
	}
	return secret
}

// BuildKerberosSecretBase is a test helper that creates the skeleton of a
// Kerberos secret.  The caller's responsibility to add the necessary data.
func BuildKerberosSecretBase(vdb *vapi.VerticaDB) *corev1.Secret {
	nm := names.GenNamespacedName(vdb, vdb.Spec.KerberosSecret)
	return BuildSecretBase(nm)
}

// BuildSecretBase is a test helper that creates a Secret base with a specific
// name.  The caller is responsible to add data elemets and create it.
func BuildSecretBase(nm types.NamespacedName) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
		},
		Data: map[string][]byte{},
	}
	return secret
}

// makeUpdateStrategy will create the updateStrategy to use for the statefulset.
func makeUpdateStrategy(vdb *vapi.VerticaDB) appsv1.StatefulSetUpdateStrategy {
	// kSafety0 needs to use the OnDelete strategy.  kSafety 0 means that as
	// soon as one pod goes down, all pods go down with it.  So we can't have a
	// rolling update strategy as it just won't work.  As soon as we delete one
	// pod, the vertica process on the other gets shut down.  We would need to
	// call admintools -t start_db after each pod gets delete and rescheduled.
	// kSafety0 is for test purposes, which is why its okay to have a different
	// strategy for it.
	if vdb.Spec.KSafety == vapi.KSafety0 {
		return appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType}
	}
	return appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType}
}

// buildCanaryQuerySQL returns the SQL to use that will check if the vertica
// process is up and accepting connections.
func buildCanaryQuerySQL(vdb *vapi.VerticaDB) string {
	passwd := ""
	if vdb.Spec.SuperuserPasswordSecret != "" {
		passwd = fmt.Sprintf("-w $(cat %s/%s)", paths.PodInfoPath, SuperuserPasswordPath)
	}

	return fmt.Sprintf("vsql %s -c 'select 1'", passwd)
}

// GetK8sLocalObjectReferenceArray returns a k8s LocalObjecReference array
// from a vapi.LocalObjectReference array
func GetK8sLocalObjectReferenceArray(lors []vapi.LocalObjectReference) []corev1.LocalObjectReference {
	localObjectReferences := []corev1.LocalObjectReference{}
	for i := range lors {
		l := corev1.LocalObjectReference{Name: lors[i].Name}
		localObjectReferences = append(localObjectReferences, l)
	}
	return localObjectReferences
}

// GetK8sAffinity returns a K8s Affinity object from a vapi.Affinity object
func GetK8sAffinity(a vapi.Affinity) *corev1.Affinity {
	return &corev1.Affinity{
		NodeAffinity:    a.NodeAffinity,
		PodAffinity:     a.PodAffinity,
		PodAntiAffinity: a.PodAntiAffinity,
	}
}
