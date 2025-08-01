/*
 (c) Copyright [2021-2024] Open Text.
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
	"maps"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	SuperuserPasswordPath   = "superuser-passwd"
	TestStorageClassName    = "test-storage-class"
	VerticaNonTLSHTTPPort   = 8080
	InternalVerticaCommPort = 5434
	SSHPort                 = 22
	VerticaClusterCommPort  = 5434
	SpreadClientPort        = 4803
	NMAPort                 = 5554
	StdOut                  = "/proc/1/fd/1"
	VProxyDefaultImage      = "opentext/client-proxy:latest"

	// Port for Vertica processes within pods
	// For the ports of the services, use:
	// vdb.spec.ServiceHTTPSPort and vdb.spec.ServiceClientPort
	VerticaClientPort = 5433
	VerticaHTTPPort   = 8443

	verticaServicePortName = "vertica-http"

	// Standard environment variables that are set in each pod
	PodIPEnv                   = "POD_IP"
	HostIPEnv                  = "HOST_IP"
	HostNameEnv                = "HOST_NODENAME"
	DataPathEnv                = "DATA_PATH"
	CatalogPathEnv             = "CATALOG_PATH"
	DepotPathEnv               = "DEPOT_PATH"
	DatabaseNameEnv            = "DATABASE_NAME"
	VSqlUserEnv                = "VSQL_USER"
	VerticaStartupLogDuplicate = "VERTICA_STARTUP_LOG_DUPLICATE"

	// Environment variables that are (optionally) set when deploying client proxy
	VProxyRootCAEnv          = "VPROXY_TLS_SERVER_CA"
	VProxyCertEnv            = "VPROXY_TLS_SERVER_CERT"
	VProxyKeyEnv             = "VPROXY_TLS_SERVER_KEY"
	VProxySecretNamespaceEnv = "VPROXY_SECRET_NAMESPACE"
	VProxySecretNameEnv      = "VPROXY_SECRET_NAME"

	// Environment variables that are (optionally) set when deployed with vclusterops
	NMARootCAEnv                = "NMA_ROOTCA_PATH"
	NMACertEnv                  = "NMA_CERT_PATH"
	NMAKeyEnv                   = "NMA_KEY_PATH"
	NMASecretNamespaceEnv       = "NMA_SECRET_NAMESPACE"        // #nosec G101
	NMASecretNameEnv            = "NMA_SECRET_NAME"             // #nosec G101
	NMAClientSecretNamespaceEnv = "NMA_CLIENT_SECRET_NAMESPACE" // #nosec G101
	NMAClientSecretNameEnv      = "NMA_CLIENT_SECRET_NAME"      // #nosec G101
	NMAClientSecretTLSModeEnv   = "NMA_CLIENT_TLS_MODE"         // #nosec G101

	// Environment variables that are set only in the nma container
	NMALogPath = "NMA_LOG_PATH"

	// HTTPS endpoint used for health check probe
	HTTPServerVersionPath = "/v1/version"

	// HTTP endpoint used for health check probe
	HTTPServerHealthPathV2 = "/v2/health"

	// Endpoint in the NMA to check its health and readiness
	NMAHealthPath = "/v1/health"

	// HTTPS endpoint used for metrics
	httpsMetricsPath = "/v1/metrics"

	// Name of the volume shared by nma and vertica containers
	startupConfMountName = "startup-conf"

	// Scrutinize constants
	scrutinizeMountName       = "scrutinize"
	scrutinizeTermGracePeriod = 0
	// Environment variables to allow vcluster scrutinize to read the password
	// from a secret
	passwordSecretNamespaceEnv = "PASSWORD_SECRET_NAMESPACE"
	passwordSecretNameEnv      = "PASSWORD_SECRET_NAME"
	// The path to the scrutinize tarball
	scrutinizeTarball = "SCRUTINIZE_TARBALL"
	passwordMountName = v1beta1.PrometheusSecretKeyPassword

	// Client proxy config file name
	vProxyConfigFile = "config.yaml"
	// Client proxy volume name
	vProxyVolumeName = "vproxy-config"
)

type ProxyData struct {
	Listener map[string]interface{}
	Database map[string][]string
	Log      map[string]string
	// TODO: to support TLS
	// Tls       map[string]string
}

// BuildExtSvc creates desired spec for the external service.
func BuildExtSvc(nm types.NamespacedName, vdb *vapi.VerticaDB, sc *vapi.Subcluster,
	selectorLabelCreator func(*vapi.VerticaDB, *vapi.Subcluster) map[string]string) *corev1.Service {
	// Use spec.subclusters[].ServiceHTTPSPort, if set
	// If not, use spec.ServiceHTTPSPort (default 8443)
	HTTPSPort := vdb.Spec.ServiceHTTPSPort
	if sc.ServiceHTTPSPort > 0 {
		HTTPSPort = sc.ServiceHTTPSPort
	}

	// Use spec.subclusters[].ServiceClientPort, if set
	// If not, use spec.ServiceClientPort (default 5433)
	ClientPort := vdb.Spec.ServiceClientPort
	if sc.ServiceClientPort > 0 {
		ClientPort = sc.ServiceClientPort
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      MakeLabelsForSvcObject(vdb, sc, vmeta.SvcTypeExternal),
			Annotations: MakeAnnotationsForSubclusterService(vdb, sc),
		},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabelCreator(vdb, sc),
			Type:     sc.ServiceType,
			Ports: []corev1.ServicePort{
				{Port: ClientPort, Name: "vertica", NodePort: sc.ClientNodePort, TargetPort: intstr.FromInt(VerticaClientPort)},
				{Port: HTTPSPort, Name: "vertica-http", NodePort: sc.VerticaHTTPNodePort, TargetPort: intstr.FromInt(VerticaHTTPPort)},
			},
			ExternalIPs:    sc.ExternalIPs,
			LoadBalancerIP: sc.LoadBalancerIP,
		},
	}
}

// BuildHlSvc creates the desired spec for the headless service.
func BuildHlSvc(nm types.NamespacedName, vdb *vapi.VerticaDB) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      MakeLabelsForSvcObject(vdb, nil, vmeta.SvcTypeHeadless),
			Annotations: MakeAnnotationsForObject(vdb),
		},
		Spec: corev1.ServiceSpec{
			Selector:                 MakeBaseSvcSelectorLabels(vdb),
			ClusterIP:                "None",
			Type:                     "ClusterIP",
			PublishNotReadyAddresses: true,
			// We must include all communication ports for vertica pods in this
			// headless service. This is needed to allow a service mesh like
			// istio to work with mTLS. That service uses the information here
			// to know what communication needs mTLS added to it. Included here
			// are the ports common regardless of deployment method. We then add
			// specific ports that are deployment method dependent a few lines down.
			Ports: []corev1.ServicePort{
				{Port: VerticaClusterCommPort, Name: "tcp-verticaclustercomm"},
				{Port: SpreadClientPort, Name: "tcp-spreadclient"},
			},
		},
	}
	if vmeta.UseVClusterOps(vdb.Annotations) {
		svc.Spec.Ports = append(svc.Spec.Ports,
			corev1.ServicePort{Port: vdb.Spec.ServiceHTTPSPort, Name: "tcp-httpservice", TargetPort: intstr.FromInt(VerticaHTTPPort)},
			corev1.ServicePort{Port: NMAPort, Name: "tcp-nma"},
		)
	} else {
		svc.Spec.Ports = append(svc.Spec.Ports,
			corev1.ServicePort{Port: SSHPort, Name: "tcp-ssh"})
	}
	return svc
}

// buildConfigVolumeMount returns the volume mount for config.
// If vclusterops flag is enabled we mount only
// /opt/vertica/config/node_management_agent.pid
func buildConfigVolumeMount(vdb *vapi.VerticaDB) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      vapi.LocalDataPVC,
		SubPath:   vdb.GetPVSubPath("config"),
		MountPath: paths.ConfigPath,
	}
}

// buildServerVolumeMounts returns the volume mounts to include
// in the server container
func buildServerVolumeMounts(vdb *vapi.VerticaDB) []corev1.VolumeMount {
	volMnts := buildVolumeMounts(vdb)
	if vdb.IsMonolithicDeploymentEnabled() {
		volMnts = append(volMnts, buildCommonNMAVolumeMounts(vdb)...)
	}
	if vdb.IsNMASideCarDeploymentEnabled() {
		volMnts = append(volMnts, buildStartupConfVolumeMount())
		// the https service needs access to the staging dir that will
		// be created under /tmp/scrutinize by NMA
		volMnts = append(volMnts, buildScrutinizeVolumeMountForVerticaPod(vdb)...)
	}
	return volMnts
}

// buildNMAVolumeMounts returns the volume mounts to include
// in the nma container
func buildNMAVolumeMounts(vdb *vapi.VerticaDB) []corev1.VolumeMount {
	volMnts := buildVolumeMounts(vdb)
	volMnts = append(volMnts, buildStartupConfVolumeMount())
	volMnts = append(volMnts, buildCommonNMAVolumeMounts(vdb)...)
	return volMnts
}

// buildVolumeMounts returns standard volume mounts common to all containers
func buildVolumeMounts(vdb *vapi.VerticaDB) []corev1.VolumeMount {
	volMnts := []corev1.VolumeMount{
		{Name: vapi.LocalDataPVC, MountPath: paths.LocalDataPath},
		buildConfigVolumeMount(vdb),
		{Name: vapi.LocalDataPVC, SubPath: vdb.GetPVSubPath("log"), MountPath: paths.LogPath},
		{Name: vapi.LocalDataPVC, SubPath: vdb.GetPVSubPath("data"), MountPath: vdb.Spec.Local.DataPath},
		{Name: vapi.PodInfoMountName, MountPath: paths.PodInfoPath},
	}
	// Only mount separate depot/catalog paths if the paths are different in the
	// container. Otherwise, you will get multiple mount points shared the same
	// path, which will prevent any pods from starting.
	// This does not apply if the depot volume is marked as not managed. In that
	// case the depot filesystem location must be provided otherwise (for example,
	// through an ephemeral PVC and the associated volumeMount)
	if vdb.Spec.Local.DataPath != vdb.Spec.Local.DepotPath && vdb.IsDepotVolumeManaged() {
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

	if vdb.Spec.HadoopConfig != "" {
		volMnts = append(volMnts, corev1.VolumeMount{
			Name:      vapi.HadoopConfigMountName,
			MountPath: paths.HadoopConfPath,
		})
	}

	if vdb.Spec.KerberosSecret != "" {
		volMnts = append(volMnts, buildKerberosVolumeMounts()...)
	}

	if vdb.GetSSHSecretName() != "" {
		volMnts = append(volMnts, buildSSHVolumeMounts()...)
	}

	volMnts = append(volMnts, buildCertSecretVolumeMounts(vdb)...)
	volMnts = append(volMnts, vdb.Spec.VolumeMounts...)

	extraPathsStr := vmeta.GetExtraLocalPaths(vdb.Annotations)
	if extraPathsStr != "" && !vmeta.DisableExtraPathsAutoMount(vdb.Annotations) {
		extraPaths := strings.Split(extraPathsStr, ",")
		for _, path := range extraPaths {
			volMnts = append(volMnts, corev1.VolumeMount{
				Name:      vapi.LocalDataPVC,
				SubPath:   filepath.Clean(vdb.GetPVSubPath(path)),
				MountPath: path,
			})
		}
	}
	return volMnts
}

func buildNMAResources(vdb *vapi.VerticaDB, sc *vapi.Subcluster) corev1.ResourceRequirements {
	// We have an option to only set the resources if the corresponding resource
	// is set in the server pod. If the server container doesn't any resources
	// set, then we won't set any defaults. This will allow us to run in
	// low-resource environment.
	forced := vmeta.IsNMAResourcesForced(vdb.Annotations)

	target := buildResources(vdb.Annotations, vmeta.GetNMAResource)
	return pickResources(&target, &sc.Resources, forced)
}

func buildScrutinizeMainContainerResources(vscr *v1beta1.VerticaScrutinize) corev1.ResourceRequirements {
	targetResources := buildResources(vscr.Annotations, vmeta.GetScrutinizeMainContainerResource)
	return pickResources(&targetResources, &vscr.Spec.Resources, false /* pick based on dependsOn */)
}

// buildResources returns a corev1.ResourceRequirements where each field is
// set using the passed function. That function is a generic function that
// extracts each resource from the annotations
func buildResources(ann map[string]string,
	getResourceFunc func(map[string]string, corev1.ResourceName) resource.Quantity) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    getResourceFunc(ann, corev1.ResourceRequestsCPU),
			corev1.ResourceMemory: getResourceFunc(ann, corev1.ResourceRequestsMemory),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    getResourceFunc(ann, corev1.ResourceLimitsCPU),
			corev1.ResourceMemory: getResourceFunc(ann, corev1.ResourceLimitsMemory),
		},
	}
}

// pickResources gets a target resources and returns a subset of it based
// on the corresponding fields set in dependsOn resources. If forced is true,
// it returns the whole target
func pickResources(target *corev1.ResourceRequirements, dependsOn *corev1.ResourceRequirements,
	forced bool) corev1.ResourceRequirements {
	if forced {
		return *target
	}

	memoryRequest := target.Requests[corev1.ResourceMemory]
	memoryLimit := target.Limits[corev1.ResourceMemory]
	cpuRequest := target.Requests[corev1.ResourceCPU]
	cpuLimit := target.Limits[corev1.ResourceCPU]

	req := corev1.ResourceRequirements{
		Requests: make(corev1.ResourceList),
		Limits:   make(corev1.ResourceList),
	}

	if _, ok := dependsOn.Requests[corev1.ResourceMemory]; ok && !memoryRequest.IsZero() {
		req.Requests[corev1.ResourceMemory] = memoryRequest
	}
	if _, ok := dependsOn.Limits[corev1.ResourceMemory]; ok && !memoryLimit.IsZero() {
		req.Limits[corev1.ResourceMemory] = memoryLimit
	}
	if _, ok := dependsOn.Requests[corev1.ResourceCPU]; ok && !cpuRequest.IsZero() {
		req.Requests[corev1.ResourceCPU] = cpuRequest
	}
	if _, ok := dependsOn.Limits[corev1.ResourceCPU]; ok && !cpuLimit.IsZero() {
		req.Limits[corev1.ResourceCPU] = cpuLimit
	}
	return req
}

func buildStartupConfVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      startupConfMountName,
		MountPath: paths.StartupConfDir,
	}
}

func buildScrutinizeVolumeMounts(vscr *v1beta1.VerticaScrutinize, vdb *vapi.VerticaDB) []corev1.VolumeMount {
	volMnts := []corev1.VolumeMount{
		buildScrutinizeSharedVolumeMount(vscr),
	}
	if vmeta.UseNMACertsMount(vdb.Annotations) &&
		vdb.GetHTTPSNMATLSSecret() != "" &&
		secrets.IsK8sSecret(vdb.GetHTTPSNMATLSSecret()) {
		volMnts = append(volMnts, buildNMACertsVolumeMount()...)
	}
	return volMnts
}

// buildScrutinizeSharedVolumeMount returns the volume mount shared by all scrutinize
// containers
func buildScrutinizeSharedVolumeMount(vscr *v1beta1.VerticaScrutinize) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      getScrutinizeVolumeMountName(vscr),
		MountPath: paths.ScrutinizeTmp,
	}
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
	// Mount the ssh key in both the dbadmin and root account. Root is needed
	// for any calls in the pod from the installer. Ideally, it would be nice to
	// mount the secret without using SubPath, since a change in the secret
	// wouldn't require a pod restart. However, admintools needs to create the
	// known_hosts file in the ssh directory. So, the ssh directory needs to be
	// writable. Mounting with SubPath is the only way to keep that path writable.
	mnts := []corev1.VolumeMount{}
	for _, p := range paths.SSHKeyPaths {
		mnts = append(mnts, []corev1.VolumeMount{
			{
				Name:      vapi.SSHMountName,
				MountPath: fmt.Sprintf("%s/%s", paths.DBAdminSSHPath, p),
				SubPath:   p,
			}, {
				Name:      vapi.SSHMountName,
				MountPath: fmt.Sprintf("%s/%s", paths.RootSSHPath, p),
				SubPath:   p,
			},
		}...)
	}
	return mnts
}

// buildCommonNMAVolumeMounts builds some extra volume mounts that are
// used with NMA
func buildCommonNMAVolumeMounts(vdb *vapi.VerticaDB) []corev1.VolumeMount {
	volMnts := buildScrutinizeVolumeMountForVerticaPod(vdb)
	if vmeta.UseNMACertsMount(vdb.Annotations) &&
		vdb.GetHTTPSNMATLSSecret() != "" &&
		secrets.IsK8sSecret(vdb.GetHTTPSNMATLSSecret()) {
		volMnts = append(volMnts, buildNMACertsVolumeMount()...)
	}
	return volMnts
}

// buildScrutinizeVolumeMountForVerticaPod builds the volume mount
// that will be used by the server and nma containers, for scrutinize
func buildScrutinizeVolumeMountForVerticaPod(vdb *vapi.VerticaDB) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			// Include a temp directory to be used by vcluster scrutinize. We want
			// the temp directory to be large enough to store compressed logs and
			// such. These can be quite big, so we cannot risk storing those in
			// local disk on the node, which may fill up and cause the pod to be
			// rescheduled
			Name:      vapi.LocalDataPVC,
			SubPath:   vdb.GetPVSubPath("scrutinize"),
			MountPath: paths.ScrutinizeTmp,
		},
	}
}

func buildNMACertsVolumeMount() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      vapi.NMACertsMountName,
			MountPath: paths.NMACertsRoot,
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
func buildVolumes(vdb *vapi.VerticaDB) []corev1.Volume {
	vols := []corev1.Volume{}
	vols = append(vols, buildPodInfoVolume(vdb))
	if vdb.Spec.LicenseSecret != "" {
		vols = append(vols, buildLicenseVolume(vdb))
	}
	if vdb.Spec.HadoopConfig != "" {
		vols = append(vols, buildHadoopConfigVolume(vdb))
	}
	if vdb.Spec.KerberosSecret != "" {
		vols = append(vols, buildKerberosVolume(vdb))
	}
	if vdb.GetSSHSecretName() != "" {
		vols = append(vols, buildSSHVolume(vdb))
	}

	if vmeta.UseVClusterOps(vdb.Annotations) &&
		vmeta.UseNMACertsMount(vdb.Annotations) &&
		vdb.GetHTTPSNMATLSSecret() != "" &&
		secrets.IsK8sSecret(vdb.GetHTTPSNMATLSSecret()) {
		vols = append(vols, buildNMACertsSecretVolume(vdb))
	}
	if vdb.IsDepotVolumeEmptyDir() && vdb.IsDepotVolumeManaged() {
		vols = append(vols, buildDepotVolume())
	}
	if vdb.IsNMASideCarDeploymentEnabled() {
		vols = append(vols, buildStartupConfVolume())
	}
	vols = append(vols, buildCertSecretVolumes(vdb)...)
	vols = append(vols, vdb.Spec.Volumes...)
	return vols
}

// buildScrutinizeVolumes returns volumes that will be used by the scrutinize pod
func buildScrutinizeVolumes(vscr *v1beta1.VerticaScrutinize, vdb *vapi.VerticaDB) []corev1.Volume {
	vols := []corev1.Volume{}
	if vmeta.UseVClusterOps(vdb.Annotations) &&
		vmeta.UseNMACertsMount(vdb.Annotations) &&
		vdb.GetHTTPSNMATLSSecret() != "" &&
		secrets.IsK8sSecret(vdb.GetHTTPSNMATLSSecret()) {
		vols = append(vols, buildNMACertsSecretVolume(vdb))
	}
	// we add a volume for the password when the password secret
	// is on k8s
	if vdb.Spec.PasswordSecret != "" &&
		secrets.IsK8sSecret(vdb.Spec.PasswordSecret) {
		vols = append(vols, buildPasswordVolume(vdb))
	}
	if vscr.Spec.Volume == nil {
		vols = append(vols, buildDefaultScrutinizeVolume())
		return vols
	}
	vols = append(vols, *vscr.Spec.Volume)
	return vols
}

func buildDefaultScrutinizeVolume() corev1.Volume {
	return buildEmptyDirVolume(scrutinizeMountName)
}

// buildPasswordVolume constructs a volume that has the password
func buildPasswordVolume(vdb *vapi.VerticaDB) corev1.Volume {
	return buildVolumeFromSecret(passwordMountName, vdb.Spec.PasswordSecret)
}

// buildVolumeFromSecret constructs a volume from a secret
func buildVolumeFromSecret(volName, secretName string) corev1.Volume {
	return corev1.Volume{
		Name: volName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
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
func buildPodInfoVolume(vdb *vapi.VerticaDB) corev1.Volume {
	projSources := []corev1.VolumeProjection{
		{DownwardAPI: buildDownwardAPIProjection()},
		// If there is a superuser password, include that in the projection
		{Secret: buildPasswordProjectionForVerticaPod(vdb)},
	}

	return buildVolumeFromProjections(projSources, vapi.PodInfoMountName)
}

// buildVolumeFromProjections constructs a volume from projections
func buildVolumeFromProjections(projSources []corev1.VolumeProjection, volName string) corev1.Volume {
	return corev1.Volume{
		Name: volName,
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
			{
				Path: "operator-deployment-method",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.annotations['%s']", "vertica.com/operator-deployment-method"),
				},
			},
			{
				Path: "operator-version",
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.labels['%s']", "app.kubernetes.io/version"),
				},
			},
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
	if vdb.Spec.PasswordSecret == "" {
		return false
	}

	// Construct each probe. If don't use the superuser password in them, then
	// it is safe to not mount this in the downward API projection.
	funcs := []func(*vapi.VerticaDB, string) *corev1.Probe{
		makeReadinessProbe, makeStartupProbe, makeLivenessProbe,
	}
	for _, f := range funcs {
		if probeContainsSuperuserPassword(f(vdb, "")) {
			return true
		}
	}
	return false
}

// buildPasswordProjectionForVerticaPod creates a projection from the password secret
// for inclusion in /etc/podinfo
func buildPasswordProjectionForVerticaPod(vdb *vapi.VerticaDB) *corev1.SecretProjection {
	if requiresSuperuserPasswordSecretMount(vdb) {
		return buildPasswordProjection(vdb)
	}
	return nil
}

// buildPasswordProjection creates a projection from the password secret
func buildPasswordProjection(vdb *vapi.VerticaDB) *corev1.SecretProjection {
	return &corev1.SecretProjection{
		LocalObjectReference: corev1.LocalObjectReference{Name: vdb.Spec.PasswordSecret},
		Items: []corev1.KeyToPath{
			{Key: names.SuperuserPasswordKey, Path: SuperuserPasswordPath},
		},
	}
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
				LocalObjectReference: corev1.LocalObjectReference{Name: vdb.Spec.HadoopConfig},
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
				SecretName: vdb.GetSSHSecretName(),
			},
		},
	}
}

func buildNMACertsSecretVolume(vdb *vapi.VerticaDB) corev1.Volume {
	return corev1.Volume{
		Name: vapi.NMACertsMountName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: vdb.GetHTTPSNMATLSSecret(),
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

func buildStartupConfVolume() corev1.Volume {
	return buildEmptyDirVolume(startupConfMountName)
}

// buildPodSpec creates a PodSpec for the statefulset
func buildPodSpec(vdb *vapi.VerticaDB, sc *vapi.Subcluster, ver string) corev1.PodSpec {
	termGracePeriod := int64(vmeta.GetTerminationGracePeriodSeconds(vdb.Annotations))
	return corev1.PodSpec{
		NodeSelector:                  sc.NodeSelector,
		Affinity:                      GetK8sAffinity(sc.Affinity),
		Tolerations:                   sc.Tolerations,
		ImagePullSecrets:              GetK8sLocalObjectReferenceArray(vdb.Spec.ImagePullSecrets),
		Containers:                    makeContainers(vdb, sc, ver),
		Volumes:                       buildVolumes(vdb),
		TerminationGracePeriodSeconds: &termGracePeriod,
		ServiceAccountName:            vdb.Spec.ServiceAccountName,
		SecurityContext:               vdb.Spec.PodSecurityContext,
	}
}

// buildScrutinizePodSpec creates a PodSpec for the scrutinize pod
func buildScrutinizePodSpec(vscr *v1beta1.VerticaScrutinize, vdb *vapi.VerticaDB, args []string) corev1.PodSpec {
	termGracePeriod := int64(scrutinizeTermGracePeriod)
	tarballName := GetTarballName(args)
	return corev1.PodSpec{
		NodeSelector:                  vscr.Spec.NodeSelector,
		Affinity:                      GetK8sAffinity(vapi.Affinity(vscr.Spec.Affinity)),
		Tolerations:                   vscr.Spec.Tolerations,
		InitContainers:                makeScrutinizeInitContainers(vscr, vdb, args, tarballName),
		Containers:                    []corev1.Container{makeScrutinizeMainContainer(vscr, tarballName)},
		Volumes:                       buildScrutinizeVolumes(vscr, vdb),
		TerminationGracePeriodSeconds: &termGracePeriod,
		RestartPolicy:                 corev1.RestartPolicy(vmeta.GetScrutinizePodRestartPolicy(vscr.Annotations)),
		SecurityContext:               vdb.Spec.PodSecurityContext,
		ServiceAccountName:            vdb.Spec.ServiceAccountName,
		ImagePullSecrets:              GetK8sLocalObjectReferenceArray(vdb.Spec.ImagePullSecrets),
	}
}

// makeVerticaContainers creates a list that contains the server container and
// the nma container(if nma sidecar deployment is enabled)
func makeVerticaContainers(vdb *vapi.VerticaDB, sc *vapi.Subcluster, ver string) []corev1.Container {
	cnts := []corev1.Container{}
	if vdb.IsNMASideCarDeploymentEnabled() {
		cnts = append(cnts, makeNMAContainer(vdb, sc))
	}
	cnts = append(cnts, makeServerContainer(vdb, sc, ver))
	return cnts
}

// makeScrutinizeInitContainers creates a list of init container specs that will be
// part of the scrutinize pod. The first container is the one that collects
// scrutinize command
func makeScrutinizeInitContainers(vscr *v1beta1.VerticaScrutinize, vdb *vapi.VerticaDB,
	args []string, tarballName string) []corev1.Container {
	cnts := []corev1.Container{makeScrutinizeInitContainer(vscr, vdb, args, tarballName)}
	for i := range vscr.Spec.InitContainers {
		c := vscr.Spec.InitContainers[i]
		c.Env = append(c.Env, buildScrutinizeTarballEnvVar(tarballName))
		c.VolumeMounts = append(c.VolumeMounts, buildScrutinizeSharedVolumeMount(vscr))
		cnts = append(cnts, c)
	}
	return cnts
}

// BuildServiceMonitor builds a service monitor for the vertica instance
func BuildServiceMonitor(nm types.NamespacedName, vdb *vapi.VerticaDB,
	basicAuthSecret string) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       nm.Namespace,
			Name:            nm.Name,
			Labels:          MakeLabelsForServiceMonitor(vdb),
			OwnerReferences: []metav1.OwnerReference{vdb.GenerateOwnerReference()},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:      verticaServicePortName,
					Path:      httpsMetricsPath,
					Scheme:    "https",
					BasicAuth: makeBasicAuthForServiceMonitor(vdb, basicAuthSecret),
					TLSConfig: makeTLSConfigForServiceMonitor(vdb),
					Interval:  monitoringv1.Duration(vdb.GetPrometheusScrapeDuration()),
				},
			},
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					vmeta.VDBInstanceLabel: vdb.Name,
				},
			},
			NamespaceSelector: monitoringv1.NamespaceSelector{
				MatchNames: []string{nm.Namespace},
			},
		},
	}
}

// BuildBasicAuthSecret constructs the secret that will be used by prometheus to authenticate
// to the vertica db
func BuildBasicAuthSecret(vdb *vapi.VerticaDB, name, username, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       vdb.Namespace,
			Name:            name,
			Annotations:     MakeAnnotationsForObject(vdb),
			Labels:          MakeCommonLabels(vdb, nil, false, false),
			OwnerReferences: []metav1.OwnerReference{vdb.GenerateOwnerReference()},
		},
		Type: corev1.SecretTypeBasicAuth,
		Data: map[string][]byte{
			names.SuperuserPasswordKey: []byte(password),
			names.SuperUserKey:         []byte(username),
		},
	}
}

func makeBasicAuthForServiceMonitor(vdb *vapi.VerticaDB, secret string) *monitoringv1.BasicAuth {
	if vdb.IsSetForTLS() {
		return nil
	}

	return &monitoringv1.BasicAuth{
		Username: corev1.SecretKeySelector{
			Key: names.SuperUserKey,
			LocalObjectReference: corev1.LocalObjectReference{
				Name: secret,
			},
		},
		Password: corev1.SecretKeySelector{
			Key: names.SuperuserPasswordKey,
			LocalObjectReference: corev1.LocalObjectReference{
				Name: secret,
			},
		},
	}
}

func makeTLSConfigForServiceMonitor(vdb *vapi.VerticaDB) *monitoringv1.TLSConfig {
	insecureSkipVerify := false
	if !vdb.IsSetForTLS() {
		insecureSkipVerify = true
		return &monitoringv1.TLSConfig{
			SafeTLSConfig: monitoringv1.SafeTLSConfig{
				InsecureSkipVerify: &insecureSkipVerify,
			},
		}
	}

	key, cert, ca := genTLSKeys(vdb)
	return &monitoringv1.TLSConfig{
		SafeTLSConfig: monitoringv1.SafeTLSConfig{
			InsecureSkipVerify: &insecureSkipVerify,
			KeySecret:          key,
			Cert:               cert,
			CA:                 ca,
		},
	}
}

func genTLSKeys(vdb *vapi.VerticaDB) (key *corev1.SecretKeySelector,
	cert monitoringv1.SecretOrConfigMap, ca monitoringv1.SecretOrConfigMap) {
	key = &corev1.SecretKeySelector{
		Key: corev1.TLSPrivateKeyKey,
		LocalObjectReference: corev1.LocalObjectReference{
			Name: vdb.GetNonEmptyHTTPSNMATLSSecret(),
		},
	}
	cert = monitoringv1.SecretOrConfigMap{
		Secret: &corev1.SecretKeySelector{
			Key: corev1.TLSCertKey,
			LocalObjectReference: corev1.LocalObjectReference{
				Name: vdb.GetNonEmptyHTTPSNMATLSSecret(),
			},
		},
	}
	ca = monitoringv1.SecretOrConfigMap{
		Secret: &corev1.SecretKeySelector{
			Key: paths.HTTPServerCACrtName,
			LocalObjectReference: corev1.LocalObjectReference{
				Name: vdb.GetNonEmptyHTTPSNMATLSSecret(),
			},
		},
	}

	return
}

// BuildHorizontalPodAutoscaler builds a manifest for the horizontal pod autoscaler.
func BuildHorizontalPodAutoscaler(nm types.NamespacedName, vas *vapi.VerticaAutoscaler) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nm.Namespace,
			Name:      nm.Name,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: vapi.GroupVersion.String(),
				Kind:       vapi.VerticaAutoscalerKind,
				Name:       vas.Name,
			},
			MinReplicas: vas.GetMinReplicas(),
			MaxReplicas: vas.Spec.CustomAutoscaler.Hpa.MaxReplicas,
			Metrics:     vas.GetHPAMetrics(),
			Behavior:    vas.Spec.CustomAutoscaler.Hpa.Behavior,
		},
	}
}

// BuildScaledObject builds a manifest for a keda scaledObject.
func BuildScaledObject(nm types.NamespacedName, vas *vapi.VerticaAutoscaler) *kedav1alpha1.ScaledObject {
	so := vas.Spec.CustomAutoscaler.ScaledObject
	scaledObject := &kedav1alpha1.ScaledObject{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   nm.Namespace,
			Name:        nm.Name,
			Annotations: MakeAnnotationsForScaledObject(vas),
		},
		Spec: kedav1alpha1.ScaledObjectSpec{
			ScaleTargetRef: &kedav1alpha1.ScaleTarget{
				APIVersion: vapi.GroupVersion.String(),
				Kind:       vapi.VerticaAutoscalerKind,
				Name:       vas.Name,
			},
			MinReplicaCount: so.MinReplicas,
			MaxReplicaCount: so.MaxReplicas,
			PollingInterval: so.PollingInterval,
			CooldownPeriod:  so.CooldownPeriod,
			Triggers:        buildTriggers(so.Metrics, vas),
		},
	}

	if so.Behavior != nil {
		scaledObject.Spec.Advanced = &kedav1alpha1.AdvancedConfig{
			HorizontalPodAutoscalerConfig: &kedav1alpha1.HorizontalPodAutoscalerConfig{
				Behavior: so.Behavior,
			},
		}
	}
	return scaledObject
}

// buildTriggers builds and return a list of scaled triggers.
func buildTriggers(metrics []vapi.ScaleTrigger, vas *vapi.VerticaAutoscaler) []kedav1alpha1.ScaleTriggers {
	triggers := make([]kedav1alpha1.ScaleTriggers, len(metrics))
	for i := range metrics {
		metric := &metrics[i]
		if metric.IsNil() {
			continue
		}
		metadata := metric.GetMetadata()
		if metric.IsPrometheusMetric() {
			metadata["namespace"] = vas.Namespace
			metadata["unsafeSsl"] = metric.GetUnsafeSslStr()
			if metric.Prometheus.AuthModes != "" {
				metadata["authModes"] = string(metric.Prometheus.AuthModes)
			}
		} else {
			metadata["containerName"] = names.ServerContainer
		}
		trigger := kedav1alpha1.ScaleTriggers{
			Type:       metric.GetType(),
			Name:       metric.Name,
			MetricType: metric.MetricType,
			Metadata:   metadata,
		}
		if metric.IsPrometheusMetric() {
			trigger.UseCachedMetrics = metric.Prometheus.UseCachedMetrics
		}
		if metric.AuthSecret != "" {
			taName := names.GenTriggerAuthenticationtName(vas, metric.AuthSecret)
			trigger.AuthenticationRef = &kedav1alpha1.AuthenticationRef{
				Name: taName.Name,
			}
		}
		triggers[i] = trigger
	}
	return triggers
}

// BuildTriggerAuthentication builds a manifest for a keda TriggerAuthentication.
func BuildTriggerAuthentication(vas *vapi.VerticaAutoscaler,
	metric *vapi.ScaleTrigger, ta types.NamespacedName, secretData map[string][]byte) *kedav1alpha1.TriggerAuthentication {
	authTargets := []kedav1alpha1.AuthSecretTargetRef{}
	switch metric.Prometheus.AuthModes {
	case vapi.PrometheusAuthBasic:
		authTargets = append(authTargets, buildTriggerAuthForBasic(metric)...)
	case vapi.PrometheusAuthBearer:
		authTargets = append(authTargets, buildTriggerAuthForBearer(metric, secretData)...)
	case vapi.PrometheusAuthTLS:
		authTargets = append(authTargets, buildTriggerAuthForTLS(metric)...)
	case vapi.PrometheusAuthCustom:
		authTargets = append(authTargets, buildTriggerAuthForCustom(metric)...)
	case vapi.PrometheusAuthTLSAndBasic:
		authTargets = append(authTargets, buildTriggerAuthForTLSAndBasic(metric)...)
	}
	return &kedav1alpha1.TriggerAuthentication{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: vas.Namespace,
			Name:      ta.Name,
		},
		Spec: kedav1alpha1.TriggerAuthenticationSpec{
			SecretTargetRef: authTargets,
		},
	}
}

// buildTriggerAuthForBasic builds a list of manifest for a keda AuthSecretTargetRef.
func buildTriggerAuthForBasic(metric *vapi.ScaleTrigger) []kedav1alpha1.AuthSecretTargetRef {
	authTargets := []kedav1alpha1.AuthSecretTargetRef{}
	// For 'basic' type, 'username' and 'password' are required fields in AuthSecret.
	authTargets = append(authTargets,
		kedav1alpha1.AuthSecretTargetRef{
			Parameter: vapi.PrometheusSecretKeyUsername,
			Name:      metric.AuthSecret,
			Key:       vapi.PrometheusSecretKeyUsername,
		},
		kedav1alpha1.AuthSecretTargetRef{
			Parameter: vapi.PrometheusSecretKeyPassword,
			Name:      metric.AuthSecret,
			Key:       vapi.PrometheusSecretKeyPassword,
		})
	return authTargets
}

// buildTriggerAuthForBearer builds a list of manifest for a keda AuthSecretTargetRef.
func buildTriggerAuthForBearer(metric *vapi.ScaleTrigger, secretData map[string][]byte) []kedav1alpha1.AuthSecretTargetRef {
	authTargets := []kedav1alpha1.AuthSecretTargetRef{}
	// For 'bearer' type, 'bearerToken' is required field in AuthSecret.
	authTargets = append(authTargets,
		kedav1alpha1.AuthSecretTargetRef{
			Parameter: vapi.PrometheusSecretKeyBearerToken,
			Name:      metric.AuthSecret,
			Key:       vapi.PrometheusSecretKeyBearerToken,
		})
	// For 'bearer' type, 'ca' is optional field in AuthSecret.
	if _, ok := secretData[vapi.PrometheusSecretKeyCa]; ok {
		authTargets = append(authTargets,
			kedav1alpha1.AuthSecretTargetRef{
				Parameter: vapi.PrometheusSecretKeyCa,
				Name:      metric.AuthSecret,
				Key:       vapi.PrometheusSecretKeyCa,
			})
	}
	return authTargets
}

// buildTriggerAuthForTLS builds a list of manifest for a keda AuthSecretTargetRef.
func buildTriggerAuthForTLS(metric *vapi.ScaleTrigger) []kedav1alpha1.AuthSecretTargetRef {
	authTargets := []kedav1alpha1.AuthSecretTargetRef{}
	// For 'tls' type, 'ca', 'cert' and 'key' are required fields in AuthSecret.
	authTargets = append(authTargets,
		kedav1alpha1.AuthSecretTargetRef{
			Parameter: vapi.PrometheusSecretKeyCa,
			Name:      metric.AuthSecret,
			Key:       vapi.PrometheusSecretKeyCa,
		},
		kedav1alpha1.AuthSecretTargetRef{
			Parameter: vapi.PrometheusSecretKeyCert,
			Name:      metric.AuthSecret,
			Key:       vapi.PrometheusSecretKeyCert,
		},
		kedav1alpha1.AuthSecretTargetRef{
			Parameter: vapi.PrometheusSecretKeyKey,
			Name:      metric.AuthSecret,
			Key:       vapi.PrometheusSecretKeyKey,
		})
	return authTargets
}

// buildTriggerAuthForCustom builds a list of manifest for a keda AuthSecretTargetRef.
func buildTriggerAuthForCustom(metric *vapi.ScaleTrigger) []kedav1alpha1.AuthSecretTargetRef {
	authTargets := []kedav1alpha1.AuthSecretTargetRef{}
	// For 'custom' type, 'customAuthHeader' and 'customAuthValue' are required fields in AuthSecret.
	authTargets = append(authTargets,
		kedav1alpha1.AuthSecretTargetRef{
			Parameter: vapi.PrometheusSecretKeyCustomAuthHeader,
			Name:      metric.AuthSecret,
			Key:       vapi.PrometheusSecretKeyCustomAuthHeader,
		},
		kedav1alpha1.AuthSecretTargetRef{
			Parameter: vapi.PrometheusSecretKeyCustomAuthValue,
			Name:      metric.AuthSecret,
			Key:       vapi.PrometheusSecretKeyCustomAuthValue,
		})
	return authTargets
}

// buildTriggerAuthForTLSAndBasic builds a list of manifest for a keda AuthSecretTargetRef.
func buildTriggerAuthForTLSAndBasic(metric *vapi.ScaleTrigger) []kedav1alpha1.AuthSecretTargetRef {
	authTargets := []kedav1alpha1.AuthSecretTargetRef{}
	// For 'tls,basic' type, 'username', 'password', 'ca', 'cert' and 'key' are required fields in AuthSecret.
	authTargets = append(authTargets, buildTriggerAuthForBasic(metric)...)
	authTargets = append(authTargets, buildTriggerAuthForTLS(metric)...)
	return authTargets
}

// BuildVProxyDeployment builds manifest for a subclusters VProxy deployment
func BuildVProxyDeployment(nm types.NamespacedName, vdb *vapi.VerticaDB, sc *vapi.Subcluster) *appsv1.Deployment {
	replicas := int32(vapi.VProxyDefaultReplicas)
	if sc.Proxy.Replicas != nil {
		replicas = *sc.Proxy.Replicas
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      MakeLabelsForVProxyObject(vdb, sc, false),
			Annotations: MakeAnnotationsForVProxyObject(vdb),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: MakeDepSelectorLabels(vdb, sc),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      MakeLabelsForVProxyObject(vdb, sc, true),
					Annotations: MakeAnnotationsForVProxyObject(vdb),
				},
				Spec: buildVProxyPodSpec(vdb, sc),
			},
			Replicas: &replicas,
		},
	}
}

// buildVProxyPodSpec creates a PodSpec for the vproxy deployment
func buildVProxyPodSpec(vdb *vapi.VerticaDB, sc *vapi.Subcluster) corev1.PodSpec {
	termGracePeriod := int64(0)
	return corev1.PodSpec{
		ImagePullSecrets:              GetK8sLocalObjectReferenceArray(vdb.Spec.ImagePullSecrets),
		Containers:                    []corev1.Container{makeVProxyContainer(vdb, sc)},
		TerminationGracePeriodSeconds: &termGracePeriod,
		ServiceAccountName:            vdb.Spec.ServiceAccountName,
		SecurityContext:               vdb.Spec.PodSecurityContext,
		Volumes: []corev1.Volume{
			{
				Name: vProxyVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: sc.GetVProxyConfigMapName(vdb)},
					},
				},
			},
		},
	}
}

// makeDataForProxyConfigMap generates a configmap data in config.yaml format.
// # Vertica Proxy Configuration File Example
//
// # Proxy's listener network address for clients connections
// listener:
//
//	host: ""  # Listen on all IP addresses of the local system
//	port: 5433
//
// # The database for proxy connections
// database:
//
//	nodes:
//	  - node1.address.com:5433
//	  - node2.address.com:5433
//	  - node3.address.com:5433
//
// # Log level: 0=TRACE|1=DEBUG|2=INFO|3=WARN|4=FATAL|5=NONE (Default INFO)
// log:
//
//	level: DEBUG
//	# OR
//	# level: 1
//
// tls:
//
//	# For proxy-server TLS
//	serverca: /Path/to/server_cacert.pem
//	hostname: db.example.com
//	proxykey: /Path/to/proxy_key.pem
//	proxycert: /Path/to/proxy_cert.pem
//
// # For client-proxy TLS
// serverkey: /Path/to/server_key.pem
// servercert: /Path/to/server_cert.pem
// clientca: /Path/to/client_cacert.pem
func makeDataForVProxyConfigMap(vdb *vapi.VerticaDB, sc *vapi.Subcluster) string {
	var nodeList []string
	port := 5433

	for i := int32(0); i < sc.Size; i++ {
		nodeItem := fmt.Sprintf("%s:%d", names.GenPodDNSName(vdb, sc, i), port)
		nodeList = append(nodeList, nodeItem)
	}

	proxyData := ProxyData{
		Listener: map[string]interface{}{
			"host": "",
			"port": port,
		},
		Database: map[string][]string{
			"nodes": nodeList,
		},
		Log: map[string]string{
			"level": vmeta.GetVProxyLogLevel(vdb.Annotations),
		},
	}

	pData, _ := yaml.Marshal(proxyData)
	return string(pData)
}

// BuildVProxyConfigMap builds a config map for client proxy
func BuildVProxyConfigMap(nm types.NamespacedName, vdb *vapi.VerticaDB, sc *vapi.Subcluster) *corev1.ConfigMap {
	immutable := false
	proxyData := makeDataForVProxyConfigMap(vdb, sc)
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            nm.Name,
			Namespace:       nm.Namespace,
			Labels:          MakeLabelsForVProxyObject(vdb, sc, false),
			Annotations:     MakeAnnotationsForVProxyObject(vdb),
			OwnerReferences: []metav1.OwnerReference{vdb.GenerateOwnerReference()},
		},
		// the data should not be immutable since the proxy database nodes could be changed
		Immutable: &immutable,
		Data: map[string]string{
			vProxyConfigFile: proxyData,
		},
	}
}

// makeProxyContainer builds the spec for the client proxy container
func makeVProxyContainer(vdb *vapi.VerticaDB, sc *vapi.Subcluster) corev1.Container {
	envVars := buildVProxyTLSCertsEnvVars(vdb)
	envVars = append(envVars, buildCommonEnvVars(vdb)...)
	vProxyImage := vapi.VProxyDefaultImage
	if vdb.Spec.Proxy != nil && vdb.Spec.Proxy.Image != "" {
		vProxyImage = vdb.Spec.Proxy.Image
	}
	resources := corev1.ResourceRequirements{}
	if sc.Proxy.Resources != nil {
		resources = *sc.Proxy.Resources
	}
	return corev1.Container{
		Image:           vProxyImage,
		ImagePullPolicy: vdb.Spec.ImagePullPolicy,
		Name:            names.ProxyContainer,
		Env:             envVars,
		Resources:       resources,
		Ports: []corev1.ContainerPort{
			{ContainerPort: VerticaClientPort, Name: "vertica"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: vProxyVolumeName, MountPath: "/config"},
		},
	}
}

// makeServerContainer builds the spec for the server container
func makeServerContainer(vdb *vapi.VerticaDB, sc *vapi.Subcluster, ver string) corev1.Container {
	envVars := translateAnnotationsToEnvVars(vdb)
	envVars = append(envVars, buildCommonEnvVars(vdb)...)
	envVars = append(envVars,
		corev1.EnvVar{Name: VerticaStartupLogDuplicate, Value: StdOut},
	)

	if vdb.IsMonolithicDeploymentEnabled() {
		envVars = append(envVars, buildNMATLSCertsEnvVars(vdb)...)
	}
	cnt := corev1.Container{
		Image:           pickImage(vdb, sc),
		ImagePullPolicy: vdb.Spec.ImagePullPolicy,
		Name:            names.ServerContainer,
		Resources:       sc.Resources,
		Ports: []corev1.ContainerPort{
			{ContainerPort: VerticaClientPort, Name: "vertica"},
			{ContainerPort: InternalVerticaCommPort, Name: "vertica-int"},
			{ContainerPort: SSHPort, Name: "ssh"},
		},
		ReadinessProbe:  makeReadinessProbe(vdb, ver),
		LivenessProbe:   makeLivenessProbe(vdb, ver),
		StartupProbe:    makeStartupProbe(vdb, ver),
		SecurityContext: makeServerSecurityContext(vdb),
		Env:             envVars,
		VolumeMounts:    buildServerVolumeMounts(vdb),
	}
	if vdb.IsNMASideCarDeploymentEnabled() {
		cnt.Command = append(cnt.Command, buildVerticaStartCommand()...)
	}
	return cnt
}

// makeNMAContainer builds the spec for the nma container
func makeNMAContainer(vdb *vapi.VerticaDB, sc *vapi.Subcluster) corev1.Container {
	envVars := buildNMATLSCertsEnvVars(vdb)
	envVars = append(envVars, buildCommonEnvVars(vdb)...)
	envVars = append(envVars,
		corev1.EnvVar{Name: NMALogPath, Value: StdOut},
	)
	envVars = append(envVars, translateAnnotationsToEnvVars(vdb)...)
	cnt := corev1.Container{
		Image:           pickImage(vdb, sc),
		ImagePullPolicy: vdb.Spec.ImagePullPolicy,
		Name:            names.NMAContainer,
		Env:             envVars,
		Resources:       buildNMAResources(vdb, sc),
		Command:         buildNMACommand(),
		VolumeMounts:    buildNMAVolumeMounts(vdb),
		ReadinessProbe:  makeNMAHealthProbe(vdb, vmeta.NMAHealthProbeReadiness),
		LivenessProbe:   makeNMAHealthProbe(vdb, vmeta.NMAHealthProbeLiveness),
		StartupProbe:    makeNMAHealthProbe(vdb, vmeta.NMAHealthProbeStartup),
	}
	if vdb.Spec.NMASecurityContext != nil {
		cnt.SecurityContext = vdb.Spec.NMASecurityContext
	}
	return cnt
}

// makeScrutinizeInitContainer builds the spec of the init container that collects
// scrutinize command
func makeScrutinizeInitContainer(vscr *v1beta1.VerticaScrutinize, vdb *vapi.VerticaDB,
	args []string, tarballName string) corev1.Container {
	cnt := corev1.Container{
		Image:        vdb.Spec.Image,
		Name:         names.ScrutinizeInitContainer,
		Command:      buildScrutinizeCmd(args),
		VolumeMounts: buildScrutinizeVolumeMounts(vscr, vdb),
		Resources:    vscr.Spec.Resources,
		Env:          buildCommonEnvVars(vdb),
	}
	if vdb.Spec.PasswordSecret != "" {
		if secrets.IsK8sSecret(vdb.Spec.PasswordSecret) {
			// we mount the password into the scrutinize init container
			// only when the password secret in on k8s
			cnt.VolumeMounts = append(cnt.VolumeMounts, corev1.VolumeMount{
				Name:      passwordMountName,
				MountPath: paths.ScrutinizeDBPasswordDir,
			})
		} else {
			// the password secret env vars are needed only when the secret
			// must be retrieved from secret stores like AWS Secrets
			// Manager or GSM
			cnt.Env = append(cnt.Env, buildScrutinizeDBPasswordEnvVars(
				names.GenNamespacedName(vscr, vdb.Spec.PasswordSecret))...)
		}
	}
	cnt.Env = append(cnt.Env, append(buildNMATLSCertsEnvVars(vdb),
		buildScrutinizeTarballEnvVar(tarballName))...)
	return cnt
}

// makeScrutinizeMainContainer builds the spec of the container that will
// be running after all init containers are completed
func makeScrutinizeMainContainer(vscr *v1beta1.VerticaScrutinize, tarballName string) corev1.Container {
	return corev1.Container{
		Image: vmeta.GetScrutinizeMainContainerImage(vscr.Annotations),
		Name:  names.ScrutinizeMainContainer,
		Command: []string{
			"bash",
			"-c",
			fmt.Sprintf("sleep %d", vmeta.GetScrutinizePodTTL(vscr.Annotations)),
		},
		Env:          []corev1.EnvVar{buildScrutinizeTarballEnvVar(tarballName)},
		Resources:    buildScrutinizeMainContainerResources(vscr),
		WorkingDir:   paths.ScrutinizeTmp,
		VolumeMounts: []corev1.VolumeMount{buildScrutinizeSharedVolumeMount(vscr)},
	}
}

// makeHTTPSVersionEndpointProbe will build an HTTPS Get probe
func makeHTTPSVersionEndpointProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   HTTPServerVersionPath,
				Port:   intstr.FromInt(VerticaHTTPPort),
				Scheme: corev1.URISchemeHTTPS,
			},
		},
	}
}

// makeHTTPVersionEndpointProbe will build an HTTP Get probe
func makeHTTPVersionEndpointProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   HTTPServerHealthPathV2,
				Port:   intstr.FromInt(VerticaNonTLSHTTPPort),
				Scheme: corev1.URISchemeHTTP,
			},
		},
	}
}

// makeVerticaClientPortProbe will build a probe that if vertica is up by seeing
// if the vertica client port is being listened on.
func makeVerticaClientPortProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(VerticaClientPort),
			},
		},
	}
}

// makeCanaryQueryProbe will build a probe that does the canary SQL query to see
// if vertica is up.
func makeCanaryQueryProbe(vdb *vapi.VerticaDB) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"bash", "-c", buildCanaryQuerySQL(vdb)},
			},
		},
	}
}

// getHTTPServerVersionEndpointProbe returns an HTTPGet probe if vclusterops
// is enabled
func getHTTPServerVersionEndpointProbe(vdb *vapi.VerticaDB, ver string) *corev1.Probe {
	if vmeta.UseVClusterOps(vdb.Annotations) {
		if vdb.IsHTTPProbeSupported(ver) {
			return makeHTTPVersionEndpointProbe()
		} else {
			return makeHTTPSVersionEndpointProbe()
		}
	}
	return nil
}

// makeDefaultReadinessOrStartupProbe will return the default probe to use for
// the readiness or startup probes. Only returns the default timeouts for the
// probe. Caller is responsible for adusting those.
func makeDefaultReadinessOrStartupProbe(vdb *vapi.VerticaDB, ver string) *corev1.Probe {
	if probe := getHTTPServerVersionEndpointProbe(vdb, ver); probe != nil {
		return probe
	}
	// If using GSM, then the superuser password is not a k8s secret. We cannot
	// use the canary query then because that depends on having the password
	// mounted in the file system. Default to just checking if the client port
	// is being listened on.
	if secrets.IsGSMSecret(vdb.Spec.PasswordSecret) {
		return makeVerticaClientPortProbe()
	}
	return makeCanaryQueryProbe(vdb)
}

// makeDefaultLivenessProbe will return the default probe to use for
// liveness probe
func makeDefaultLivenessProbe(vdb *vapi.VerticaDB, ver string) *corev1.Probe {
	if probe := getHTTPServerVersionEndpointProbe(vdb, ver); probe != nil {
		return probe
	}
	// We check if the TCP client port is open. We used this approach,
	// rather than issuing 'select 1' like readinessProbe because we need
	// to minimize variability. If the livenessProbe fails, the pod is
	// rescheduled. So, it isn't as forgiving as the readinessProbe.
	return makeVerticaClientPortProbe()
}

// makeReadinessProbe will build the readiness probe. It has a default probe
// that can be overridden with the spec.readinessProbeOverride parameter.
func makeReadinessProbe(vdb *vapi.VerticaDB, ver string) *corev1.Probe {
	probe := makeDefaultReadinessOrStartupProbe(vdb, ver)
	probe.SuccessThreshold = 1
	overrideProbe(probe, vdb.Spec.ReadinessProbeOverride)
	return probe
}

// makeStartupProbe will return the Probe object to use for the startup probe.
func makeStartupProbe(vdb *vapi.VerticaDB, ver string) *corev1.Probe {
	probe := makeDefaultReadinessOrStartupProbe(vdb, ver)
	// We want to wait about 20 minutes for the server to come up before the
	// other probes come into affect. The total length of the probe is more or
	// less: InitialDelaySeconds + PeriodSeconds * FailureThreshold.
	probe.InitialDelaySeconds = 30
	probe.PeriodSeconds = 10
	probe.FailureThreshold = 117
	probe.TimeoutSeconds = 5
	probe.SuccessThreshold = 1

	overrideProbe(probe, vdb.Spec.StartupProbeOverride)
	return probe
}

// makeLivenessProbe will return the Probe object to use for the liveness probe.
func makeLivenessProbe(vdb *vapi.VerticaDB, ver string) *corev1.Probe {
	probe := makeDefaultLivenessProbe(vdb, ver)
	// These values were picked so that we can estimate how long vertica
	// needs to be unresponsive before it gets killed. We are targeting
	// about 2.5 minutes after initial start and 1.5 minutes if the pod has
	// been running for a while. The formula is:
	// InitialDelaySeconds + PeriodSeconds * FailureThreshold.
	// Note: InitialDelaySeconds only applies the first time after pod scheduling.
	probe.InitialDelaySeconds = 60
	probe.TimeoutSeconds = 1
	probe.PeriodSeconds = 30
	probe.FailureThreshold = 3
	probe.SuccessThreshold = 1

	overrideProbe(probe, vdb.Spec.LivenessProbeOverride)
	return probe
}

// makeNMAHealthProbe will return the Probe object to use for the NMA
func makeNMAHealthProbe(vdb *vapi.VerticaDB, probeName string) *corev1.Probe {
	probe := makeDefaultNMAHealthProbe()
	if val, ok := vmeta.GetNMAHealthProbeOverride(vdb.Annotations, probeName, vmeta.NMAHealthProbeInitialDelaySeconds); ok {
		probe.InitialDelaySeconds = val
	}
	if val, ok := vmeta.GetNMAHealthProbeOverride(vdb.Annotations, probeName, vmeta.NMAHealthProbeTimeoutSeconds); ok {
		probe.TimeoutSeconds = val
	}
	if val, ok := vmeta.GetNMAHealthProbeOverride(vdb.Annotations, probeName, vmeta.NMAHealthProbePeriodSeconds); ok {
		probe.PeriodSeconds = val
	}
	if val, ok := vmeta.GetNMAHealthProbeOverride(vdb.Annotations, probeName, vmeta.NMAHealthProbeSuccessThreshold); ok {
		probe.SuccessThreshold = val
	}
	if val, ok := vmeta.GetNMAHealthProbeOverride(vdb.Annotations, probeName, vmeta.NMAHealthProbeFailureThreshold); ok {
		probe.FailureThreshold = val
	}
	return probe
}

func makeDefaultNMAHealthProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   NMAHealthPath,
				Port:   intstr.FromInt(NMAPort),
				Scheme: corev1.URISchemeHTTPS,
			},
		},
	}
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

	// In vclusterops mode, we don't need SYS_CHROOT
	// and AUDIT_WRITE to run on OpenShift
	if vmeta.UseVClusterOps(vdb.Annotations) {
		return sc
	}

	if sc.Capabilities == nil {
		sc.Capabilities = &corev1.Capabilities{}
	}
	capabilitiesNeeded := []corev1.Capability{
		// Needed to run sshd on OpenShift
		"SYS_CHROOT",
		// Needed to run sshd on OpenShift
		"AUDIT_WRITE",
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
func makeContainers(vdb *vapi.VerticaDB, sc *vapi.Subcluster, ver string) []corev1.Container {
	cnts := makeVerticaContainers(vdb, sc, ver)
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
func BuildStsSpec(nm types.NamespacedName, vdb *vapi.VerticaDB, sc *vapi.Subcluster, ver string) *appsv1.StatefulSet {
	scSize := sc.GetStsSize(vdb)
	ownerRef := []metav1.OwnerReference{vdb.GenerateOwnerReference()}
	// when preserveDBDirectory is enabled, we don't want PVCs to be owned by VerticaDB
	if vmeta.GetPreserveDBDirectory(vdb.Annotations) {
		ownerRef = nil
	}
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      MakeLabelsForStsObject(vdb, sc),
			Annotations: MakeAnnotationsForStsObject(vdb, sc),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: MakeStsSelectorLabels(vdb, sc),
			},
			ServiceName: names.GenHlSvcName(vdb).Name,
			Replicas:    &scSize,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      MakeLabelsForPodObject(vdb, sc),
					Annotations: MakeAnnotationsForPod(vdb),
				},
				Spec: buildPodSpec(vdb, sc, ver),
			},
			UpdateStrategy:      makeUpdateStrategy(vdb),
			PodManagementPolicy: appsv1.ParallelPodManagement,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: vapi.LocalDataPVC,
						// Set the ownerReference so that we get auto-deletion
						OwnerReferences: ownerRef,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: getStorageClassName(vdb),
						Resources: corev1.VolumeResourceRequirements{
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

// BuildSandboxConfigMap builds a config map for sandbox controller
func BuildSandboxConfigMap(nm types.NamespacedName, vdb *vapi.VerticaDB, sandbox string, disableRouting bool) *corev1.ConfigMap {
	immutable := true
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            nm.Name,
			Namespace:       nm.Namespace,
			Labels:          MakeLabelsForSandboxConfigMap(vdb),
			Annotations:     MakeAnnotationsForSandboxConfigMap(vdb, disableRouting),
			OwnerReferences: []metav1.OwnerReference{vdb.GenerateOwnerReference()},
		},
		// the data should be immutable since dbName and sandboxName are fixed
		Immutable: &immutable,
		Data: map[string]string{
			vapi.VerticaDBNameKey: vdb.Name,
			vapi.SandboxNameKey:   sandbox,
		},
	}
}

// BuildScrutinizePod construct the spec for the scrutinize pod
func BuildScrutinizePod(vscr *v1beta1.VerticaScrutinize, vdb *vapi.VerticaDB, args []string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        vscr.Name,
			Namespace:   vscr.Namespace,
			Labels:      maps.Clone(vscr.Spec.Labels),
			Annotations: maps.Clone(vscr.Spec.Annotations),
		},
		Spec: buildScrutinizePodSpec(vscr, vdb, args),
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
		Spec: buildPodSpec(vdb, sc, ""),
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
			Resources: corev1.VolumeResourceRequirements{
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

// BuildCommunalCredSecret is a test helper to build up the Secret spec to store communal credentials
func BuildCommunalCredSecret(vdb *vapi.VerticaDB, accessKey, secretKey string) *corev1.Secret {
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
	// start the cluster after each pod gets delete and rescheduled.
	// kSafety0 is for test purposes, which is why its okay to have a different
	// strategy for it.
	if vdb.IsKSafety0() {
		return appsv1.StatefulSetUpdateStrategy{Type: appsv1.OnDeleteStatefulSetStrategyType}
	}
	return appsv1.StatefulSetUpdateStrategy{Type: appsv1.RollingUpdateStatefulSetStrategyType}
}

// buildCanaryQuerySQL returns the SQL to use that will check if the vertica
// process is up and accepting connections.
func buildCanaryQuerySQL(vdb *vapi.VerticaDB) string {
	passwd := ""
	if vdb.Spec.PasswordSecret != "" {
		passwd = fmt.Sprintf("-w $(cat %s/%s)", paths.PodInfoPath, SuperuserPasswordPath)
	}

	if vmeta.UseTLSAuth(vdb.Annotations) {
		return fmt.Sprintf("vsql %s -m allow -c 'select 1'", passwd)
	}
	return fmt.Sprintf("vsql %s -c 'select 1'", passwd)
}

// buildVerticaStartCommand returns the vertica start command that
// will serve as entrypoint to the server container
func buildVerticaStartCommand() []string {
	return []string{
		"/opt/vertica/bin/vertica", "--startup-conf", paths.StartupConfFile,
	}
}

// buildNMACommand returns the command to start NMA
func buildNMACommand() []string {
	return []string{
		"/opt/vertica/bin/node_management_agent",
	}
}

// buildScrutinizeCmd returns the full vcluster scrutinize command
func buildScrutinizeCmd(args []string) []string {
	cmd := []string{
		"/opt/vertica/bin/vcluster",
		"scrutinize",
	}
	cmd = append(cmd, args...)
	return cmd
}

// buildScrutinizeDBpasswordEnvVars returns environment variables that are needed
// by scrutinize to read password from secret
func buildScrutinizeDBPasswordEnvVars(nm types.NamespacedName) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: passwordSecretNamespaceEnv, Value: nm.Namespace},
		{Name: passwordSecretNameEnv, Value: nm.Name},
	}
}

// buildNMATLSCertsEnvVars returns environment variables about NMA certs,
// that are needed by NMA and vcluster scrutinize
func buildNMATLSCertsEnvVars(vdb *vapi.VerticaDB) []corev1.EnvVar {
	True := true
	configMapName := fmt.Sprintf("%s-%s", vdb.Name, vapi.NMATLSConfigMapName)
	envs := []corev1.EnvVar{}
	useNmaCertsMount := vmeta.UseNMACertsMount(vdb.Annotations)
	if useNmaCertsMount && secrets.IsK8sSecret(vdb.GetHTTPSNMATLSSecret()) {
		envs = append(envs,
			// Provide the path to each of the certs that are mounted in the container.
			corev1.EnvVar{Name: NMARootCAEnv, Value: fmt.Sprintf("%s/%s", paths.NMACertsRoot, paths.HTTPServerCACrtName)},
			corev1.EnvVar{Name: NMACertEnv, Value: fmt.Sprintf("%s/%s", paths.NMACertsRoot, corev1.TLSCertKey)},
			corev1.EnvVar{Name: NMAKeyEnv, Value: fmt.Sprintf("%s/%s", paths.NMACertsRoot, corev1.TLSPrivateKeyKey)},
		)
		return envs
	}

	envs = append(envs,
		corev1.EnvVar{Name: NMASecretNamespaceEnv,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Key:      NMASecretNamespaceEnv,
					Optional: &True,
				},
			}},
		corev1.EnvVar{Name: NMASecretNameEnv,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Key:      NMASecretNameEnv,
					Optional: &True,
				},
			}},
		corev1.EnvVar{Name: NMAClientSecretNamespaceEnv,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Key:      NMAClientSecretNamespaceEnv,
					Optional: &True,
				},
			}},
		corev1.EnvVar{Name: NMAClientSecretNameEnv,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Key:      NMAClientSecretNameEnv,
					Optional: &True,
				},
			}},
		corev1.EnvVar{Name: NMAClientSecretTLSModeEnv,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Key:      NMAClientSecretTLSModeEnv,
					Optional: &True,
				},
			}},
	)
	return envs
}

// buildVProxyTLSCertsEnvVars returns environment variables about proxy certs
func buildVProxyTLSCertsEnvVars(vdb *vapi.VerticaDB) []corev1.EnvVar {
	if vmeta.UseVProxyCertsMount(vdb.Annotations) && secrets.IsK8sSecret(vdb.Spec.Proxy.TLSSecret) {
		return []corev1.EnvVar{
			// TODO: use proxy certs
		}
	}
	return []corev1.EnvVar{
		// The proxy will read the secrets directly from the secret store.
		// We provide the secret namespace and name for this reason.
		{Name: VProxySecretNamespaceEnv, Value: vdb.ObjectMeta.Namespace},
		{Name: VProxySecretNameEnv, Value: vdb.Spec.Proxy.TLSSecret},
	}
}

// buildCommonEnvVars returns env vars that are common for the nma and server container.
func buildCommonEnvVars(vdb *vapi.VerticaDB) []corev1.EnvVar {
	return []corev1.EnvVar{
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
		{Name: VSqlUserEnv, Value: vdb.GetVerticaUser()},
	}
}

// buildScrutinizeTarballEnvVar returns the environment variable about
// the path to the scrutinize tarball
func buildScrutinizeTarballEnvVar(tarballName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  scrutinizeTarball,
		Value: getScrutinizeTarballFullPath(tarballName),
	}
}

func getScrutinizeTarballFullPath(tarballName string) string {
	return fmt.Sprintf("%s/%s", paths.ScrutinizeTmp, tarballName)
}

func getScrutinizeVolumeMountName(vscr *v1beta1.VerticaScrutinize) string {
	if vscr.Spec.Volume == nil {
		return scrutinizeMountName
	}
	return vscr.Spec.Volume.Name
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

// GetTarballName extracts the tarball name from a slice of strings
// containing scutinize command arguments
func GetTarballName(cmd []string) string {
	for i := range cmd {
		if cmd[i] == "--tarball-name" && i < len(cmd)-1 {
			return fmt.Sprintf("%s.tar", cmd[i+1])
		}
	}
	return ""
}

// BuildNMATLSConfigMap builds a configmap with tls secret name in it.
// The configmap will be mapped to two environmental variables in NMA pod
func BuildNMATLSConfigMap(nm types.NamespacedName, vdb *vapi.VerticaDB) *corev1.ConfigMap {
	secretMap := map[string]string{
		NMASecretNamespaceEnv:       vdb.ObjectMeta.Namespace,
		NMASecretNameEnv:            vdb.GetHTTPSNMATLSSecretForConfigMap(),
		NMAClientSecretNamespaceEnv: vdb.ObjectMeta.Namespace,
		NMAClientSecretNameEnv:      vdb.GetClientServerTLSSecretForConfigMap(),
		NMAClientSecretTLSModeEnv:   vdb.GetNMAClientServerTLSMode(),
	}
	tlsConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            nm.Name,
			Namespace:       nm.Namespace,
			OwnerReferences: []metav1.OwnerReference{vdb.GenerateOwnerReference()},
		},
		Data: secretMap,
	}
	return tlsConfigMap
}

// BuildServerHealthProbes builds the liveness, readiness and startup probes
func BuildServerHealthProbes(vdb *vapi.VerticaDB, ver string) (readnesProbe, livenessProbe, startupProbe *corev1.Probe) {
	return makeReadinessProbe(vdb, ver), makeLivenessProbe(vdb, ver), makeStartupProbe(vdb, ver)
}
