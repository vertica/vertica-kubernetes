/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

import (
	"fmt"
	"path/filepath"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	SuperuserPasswordPath          = "superuser-passwd"
	DefaultTransientSubclusterName = "transient"
)

// buildExtSvc creates desired spec for the external service.
func buildExtSvc(nm types.NamespacedName, vdb *vapi.VerticaDB, sc *vapi.Subcluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      makeLabelsForSvcObject(vdb, sc, "external"),
			Annotations: makeAnnotationsForObject(vdb),
		},
		Spec: corev1.ServiceSpec{
			Selector: makeSvcSelectorLabels(vdb, sc),
			Type:     sc.ServiceType,
			Ports: []corev1.ServicePort{
				{Port: 5433, Name: "vertica", NodePort: sc.NodePort},
				{Port: 5444, Name: "agent"},
			},
			ExternalIPs: sc.ExternalIPs,
		},
	}
}

// buildHlSvc creates the desired spec for the headless service.
func buildHlSvc(nm types.NamespacedName, vdb *vapi.VerticaDB) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      makeLabelsForSvcObject(vdb, nil, "headless"),
			Annotations: makeAnnotationsForObject(vdb),
		},
		Spec: corev1.ServiceSpec{
			Selector:                 makeSvcSelectorLabels(vdb, nil),
			ClusterIP:                "None",
			Type:                     "ClusterIP",
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{Port: 22, Name: "ssh"},
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
		{Name: vapi.LocalDataPVC, SubPath: vdb.GetPVSubPath("depot"), MountPath: vdb.Spec.Local.DepotPath},
		{Name: vapi.PodInfoMountName, MountPath: paths.PodInfoPath},
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
	if vdb.Spec.Communal.HadoopConfig != "" {
		vols = append(vols, buildHadoopConfigVolume(vdb))
	}
	if vdb.Spec.KerberosSecret != "" {
		vols = append(vols, buildKerberosVolume(vdb))
	}
	if vdb.Spec.SSHSecret != "" {
		vols = append(vols, buildSSHVolume(vdb))
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
func buildPodInfoVolume(vdb *vapi.VerticaDB) corev1.Volume {
	projSources := []corev1.VolumeProjection{
		{
			DownwardAPI: &corev1.DownwardAPIProjection{
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
						Path: "name",
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
			},
		},
	}

	// If these is a superuser password, include that in the projection
	if vdb.Spec.SuperuserPasswordSecret != "" {
		secretProj := &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: vdb.Spec.SuperuserPasswordSecret},
			Items: []corev1.KeyToPath{
				{Key: SuperuserPasswordKey, Path: SuperuserPasswordPath},
			},
		}
		projSources = append(projSources, corev1.VolumeProjection{Secret: secretProj})
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

// buildPodSpec creates a PodSpec for the statefulset
func buildPodSpec(vdb *vapi.VerticaDB, sc *vapi.Subcluster) corev1.PodSpec {
	termGracePeriod := int64(0)
	return corev1.PodSpec{
		NodeSelector:                  sc.NodeSelector,
		Affinity:                      getK8sAffinity(sc.Affinity),
		Tolerations:                   sc.Tolerations,
		ImagePullSecrets:              getK8sLocalObjectReferenceArray(vdb.Spec.ImagePullSecrets),
		Containers:                    makeContainers(vdb, sc),
		Volumes:                       buildVolumes(vdb),
		TerminationGracePeriodSeconds: &termGracePeriod,
		ServiceAccountName:            "verticadb-operator-controller-manager",
	}
}

// makeServerContainer builds the spec for the server container
func makeServerContainer(vdb *vapi.VerticaDB, sc *vapi.Subcluster) corev1.Container {
	return corev1.Container{
		Image:           pickImage(vdb, sc),
		ImagePullPolicy: vdb.Spec.ImagePullPolicy,
		Name:            names.ServerContainer,
		Resources:       sc.Resources,
		Ports: []corev1.ContainerPort{
			{ContainerPort: 5433, Name: "vertica"},
			{ContainerPort: 5434, Name: "vertica-int"},
			{ContainerPort: 22, Name: "ssh"},
		},
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{"bash", "-c", buildReadinessProbeSQL(vdb)},
				},
			},
		},
		// Is needed to run sshd on Openshift
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_CHROOT", "AUDIT_WRITE"},
			},
		},
		Env: []corev1.EnvVar{
			{Name: "POD_IP", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"}},
			},
			{Name: "DATA_PATH", Value: vdb.Spec.Local.DataPath},
			{Name: "DEPOT_PATH", Value: vdb.Spec.Local.DepotPath},
		},
		VolumeMounts: buildVolumeMounts(vdb),
	}
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
		// As a convenience, add the database path as an environment variable.
		c.Env = append(c.Env, corev1.EnvVar{Name: "DBPATH", Value: vdb.GetDBDataPath()})
		cnts = append(cnts, c)
	}
	return cnts
}

// pickImage will pick the correct image for the subcluster to use
func pickImage(vdb *vapi.VerticaDB, sc *vapi.Subcluster) string {
	// The ImageOverride exists to allow standby subclusters created for
	// primaries to continue to use the old image during an online image change.
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

// buildStsSpec builds manifest for a subclusters statefulset
func buildStsSpec(nm types.NamespacedName, vdb *vapi.VerticaDB, sc *vapi.Subcluster) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      makeLabelsForObject(vdb, sc),
			Annotations: makeAnnotationsForObject(vdb),
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: makeSvcSelectorLabels(vdb, sc),
			},
			ServiceName: names.GenHlSvcName(vdb).Name,
			Replicas:    &sc.Size,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      makeLabelsForObject(vdb, sc),
					Annotations: makeAnnotationsForObject(vdb),
				},
				Spec: buildPodSpec(vdb, sc),
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
								"storage": vdb.Spec.Local.RequestSize,
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
func buildPod(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) *corev1.Pod {
	nm := names.GenPodName(vdb, sc, podIndex)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			Labels:      makeLabelsForObject(vdb, sc),
			Annotations: makeAnnotationsForObject(vdb),
		},
		Spec: buildPodSpec(vdb, sc),
	}
	// Set a few things in the spec that are normally done by the statefulset
	// controller. Again, this is for testing purposes only as the statefulset
	// controller handles adding of the PVC to the volume list.
	pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
		Name: vapi.LocalDataPVC,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: vapi.LocalDataPVC + "-" + vdb.ObjectMeta.Name + "-" + sc.Name + fmt.Sprintf("%d", podIndex),
			},
		},
	})
	pod.Spec.Hostname = nm.Name
	pod.Spec.Subdomain = names.GenHlSvcName(vdb).Name
	return pod
}

// buildS3CommunalCredSecret is a test helper to build up the Secret spec to store communal credentials
func buildS3CommunalCredSecret(vdb *vapi.VerticaDB, accessKey, secretKey string) *corev1.Secret {
	nm := names.GenCommunalCredSecretName(vdb)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
		},
		Data: map[string][]byte{
			CommunalAccessKeyName: []byte(accessKey),
			CommunalSecretKeyName: []byte(secretKey),
		},
	}
	return secret
}

// buildAzureAccountKeyCommunalCredSecret builds a secret that is setup for
// Azure using an account key.
func buildAzureAccountKeyCommunalCredSecret(vdb *vapi.VerticaDB, accountName, accountKey string) *corev1.Secret {
	nm := names.GenCommunalCredSecretName(vdb)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
		},
		Data: map[string][]byte{
			AzureAccountName: []byte(accountName),
			AzureAccountKey:  []byte(accountKey),
		},
	}
	return secret
}

// buildAzureSASCommunalCredSecret builds a secret that is setup for Azure using
// shared access signature.
func buildAzureSASCommunalCredSecret(vdb *vapi.VerticaDB, blobEndpoint, sas string) *corev1.Secret {
	nm := names.GenCommunalCredSecretName(vdb)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
		},
		Data: map[string][]byte{
			AzureBlobEndpoint:          []byte(blobEndpoint),
			AzureSharedAccessSignature: []byte(sas),
		},
	}
	return secret
}

// buildKerberosSecretBase is a test helper that creates the skeleton of a
// Kerberos secret.  The caller's responsibility to add the necessary data.
func buildKerberosSecretBase(vdb *vapi.VerticaDB) *corev1.Secret {
	nm := names.GenNamespacedName(vdb, vdb.Spec.KerberosSecret)
	return buildSecretBase(nm)
}

// buildSecretBase is a test helper that creates a Secret base with a specific
// name.  The caller is responsible to add data elemets and create it.
func buildSecretBase(nm types.NamespacedName) *corev1.Secret {
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

// buildReadinessProbeSQL returns the SQL to use that will check if the pod is ready.
func buildReadinessProbeSQL(vdb *vapi.VerticaDB) string {
	passwd := ""
	if vdb.Spec.SuperuserPasswordSecret != "" {
		passwd = fmt.Sprintf("-w $(cat %s/%s)", paths.PodInfoPath, SuperuserPasswordPath)
	}

	return fmt.Sprintf("vsql %s -c 'select 1'", passwd)
}

// getK8sLocalObjectReferenceArray returns a k8s LocalObjecReference array
// from a vapi.LocalObjectReference array
func getK8sLocalObjectReferenceArray(lors []vapi.LocalObjectReference) []corev1.LocalObjectReference {
	localObjectReferences := []corev1.LocalObjectReference{}
	for i := range lors {
		l := corev1.LocalObjectReference{Name: lors[i].Name}
		localObjectReferences = append(localObjectReferences, l)
	}
	return localObjectReferences
}

// getK8sAffinity returns a K8s Affinity object from a vapi.Affinity object
func getK8sAffinity(a vapi.Affinity) *corev1.Affinity {
	return &corev1.Affinity{
		NodeAffinity:    a.NodeAffinity,
		PodAffinity:     a.PodAffinity,
		PodAntiAffinity: a.PodAntiAffinity,
	}
}

// buildTransientSubcluster creates a temporary read-only subcluster based on an
// existing subcluster
func buildTransientSubcluster(vdb *vapi.VerticaDB, sc *vapi.Subcluster, imageOverride string) *vapi.Subcluster {
	return &vapi.Subcluster{
		Name:              transientSubclusterName(vdb),
		Size:              transientSubclusterSize(vdb),
		IsTransient:       true,
		ImageOverride:     imageOverride,
		IsPrimary:         false,
		NodeSelector:      vdb.Spec.TransientSubclusterTemplate.NodeSelector,
		Affinity:          vdb.Spec.TransientSubclusterTemplate.Affinity,
		PriorityClassName: vdb.Spec.TransientSubclusterTemplate.PriorityClassName,
		Tolerations:       vdb.Spec.TransientSubclusterTemplate.Tolerations,
		Resources:         vdb.Spec.TransientSubclusterTemplate.Resources,
		ServiceType:       sc.ServiceType,
		ServiceName:       sc.GetServiceName(),
		NodePort:          sc.NodePort,
		ExternalIPs:       sc.ExternalIPs,
	}
}

func transientSubclusterName(vdb *vapi.VerticaDB) string {
	if vdb.Spec.TransientSubclusterTemplate.Name == "" {
		return DefaultTransientSubclusterName
	}
	return vdb.Spec.TransientSubclusterTemplate.Name
}

func transientSubclusterSize(vdb *vapi.VerticaDB) int32 {
	if vdb.Spec.TransientSubclusterTemplate.Size > 0 {
		return vdb.Spec.TransientSubclusterTemplate.Size
	}
	return 1
}
