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

package names

import (
	"fmt"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ServerContainer         = "server"
	NMAContainer            = "nma"
	ProxyContainer          = "proxy"
	ScrutinizeInitContainer = "scrutinize"
	ScrutinizeMainContainer = "main"
)

const (
	// The name of the key in the superuser password secret that holds the password
	SuperuserPasswordKey = "password"
	SuperUserKey         = "username"
)

// GenNamespacedName will take any name and make it a namespace name that uses
// the same namespace as the k8s object.
func GenNamespacedName(obj client.Object, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: obj.GetNamespace(),
	}
}

// GenExtSvcName returns the name of the external service object.
func GenExtSvcName(vdb *vapi.VerticaDB, sc *vapi.Subcluster) types.NamespacedName {
	return GenNamespacedName(vdb, vdb.Name+"-"+sc.GetServiceName())
}

// GenHlSvcName returns the name of the headless service object.
func GenHlSvcName(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, vdb.Name)
}

// GenStsName returns the name of the statefulset object
func GenStsName(vdb *vapi.VerticaDB, sc *vapi.Subcluster) types.NamespacedName {
	return GenNamespacedName(vdb, sc.GetStatefulSetName(vdb))
}

// GenSandboxConfigMapName returns the name of the sandbox config map
func GenSandboxConfigMapName(vdb *vapi.VerticaDB, sandbox string) types.NamespacedName {
	return GenNamespacedName(vdb, vdb.Name+"-"+vapi.GenCompatibleFQDNHelper(sandbox))
}

// GenVProxyName returns the name of the client proxy deployment
func GenVProxyName(vdb *vapi.VerticaDB, s *vapi.Subcluster) types.NamespacedName {
	return GenNamespacedName(vdb, s.GetVProxyDeploymentName(vdb))
}

// GenVProxyConfigMapName returns the name of the client proxy configmap
func GenVProxyConfigMapName(vdb *vapi.VerticaDB, s *vapi.Subcluster) types.NamespacedName {
	return GenNamespacedName(vdb, s.GetVProxyConfigMapName(vdb))
}

// GenCommunalCredSecretName returns the name of the secret that has the credentials to access s3
func GenCommunalCredSecretName(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, vdb.Spec.Communal.CredentialSecret)
}

// GenS3SseCustomerKeySecretName returns the name of the secret that has the client key
// for SSE-C server-side encryption.
func GenS3SseCustomerKeySecretName(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, vdb.Spec.Communal.S3SseCustomerKeySecret)
}

// GenKrb5SecretName returns the name of the secret that has the Kerberos config and keytab
func GenKrb5SecretName(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, vdb.Spec.KerberosSecret)
}

// GenSUPasswdSecretName returns the name of the secret specified in vdb that has the superuser password
func GenSUPasswdSecretName(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, vdb.Spec.PasswordSecret)
}

// GenPodName returns the name of a specific pod in a subcluster
// The name of the pod is generated, this function is just a helper for when we need
// to lookup a pod by its generated name.
func GenPodName(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) types.NamespacedName {
	stsName := sc.GetStatefulSetName(vdb)
	return GenNamespacedName(vdb, fmt.Sprintf("%s-%d", stsName, podIndex))
}

// GenPodDNSName returns the DNS name of a specific pod
func GenPodDNSName(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) string {
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", GenPodName(vdb, sc, podIndex).Name, vdb.Name, vdb.Namespace)
}

// GenPodNameFromSts returns the name of a specific pod in a statefulset
func GenPodNameFromSts(vdb *vapi.VerticaDB, sts *appsv1.StatefulSet, podIndex int32) types.NamespacedName {
	return types.NamespacedName{
		Name:      fmt.Sprintf("%s-%d", sts.GetObjectMeta().GetName(), podIndex),
		Namespace: vdb.Namespace,
	}
}

// GenPVCName returns the name of a specific pod's PVC.  This is for test purposes only.
func GenPVCName(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) types.NamespacedName {
	return GenNamespacedName(vdb, fmt.Sprintf("%s-%s-%d", vapi.LocalDataPVC, sc.GetStatefulSetName(vdb), podIndex))
}

// GenPVName returns the name of a dummy PV for test purposes
func GenPVName(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) types.NamespacedName {
	return types.NamespacedName{
		Name: fmt.Sprintf("pv-%s-%s-%d", vapi.LocalDataPVC, sc.GetStatefulSetName(vdb), podIndex),
	}
}

func GenHPAName(vas *vapi.VerticaAutoscaler) types.NamespacedName {
	return GenNamespacedName(vas, fmt.Sprintf("%s-hpa", vas.Name))
}

func GenScaledObjectName(vas *vapi.VerticaAutoscaler) types.NamespacedName {
	return GenNamespacedName(vas, fmt.Sprintf("%s-keda", vas.Name))
}

func GenAuthSecretName(vas *vapi.VerticaAutoscaler, secretName string) types.NamespacedName {
	return GenNamespacedName(vas, secretName)
}

func GenTriggerAuthenticationtName(vas *vapi.VerticaAutoscaler, secretName string) types.NamespacedName {
	return GenNamespacedName(vas, fmt.Sprintf("%s-%s-creds", vas.Name, secretName))
}

func GenNMACertConfigMap(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, fmt.Sprintf("%s-%s", vdb.Name, vapi.NMATLSConfigMapName))
}

func GenBasicauthSecretName(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, fmt.Sprintf("%s-basic-auth", vdb.Name))
}

func GenSvcMonitorName(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, fmt.Sprintf("%s-svc-monitor", vdb.Name))
}
