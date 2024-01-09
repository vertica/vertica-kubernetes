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

package names

import (
	"fmt"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ServerContainer = "server"
	NMAContainer    = "nma"
)

const (
	firstContainerIndex = iota
	serverContainerIndex
)

// GenNamespacedName will take any name and make it a namespace name that uses
// the same namespace as the VerticaDB.
func GenNamespacedName(vdb *vapi.VerticaDB, name string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: vdb.Namespace,
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
	return GenNamespacedName(vdb, vdb.Name+"-"+sc.GenCompatibleFQDN())
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

// GenSUPasswdSecretName returns the name of the secret that has the superuser password
func GenSUPasswdSecretName(vdb *vapi.VerticaDB) types.NamespacedName {
	return GenNamespacedName(vdb, vdb.Spec.PasswordSecret)
}

// GenPodName returns the name of a specific pod in a subcluster
// The name of the pod is generated, this function is just a helper for when we need
// to lookup a pod by its generated name.
func GenPodName(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) types.NamespacedName {
	return GenNamespacedName(vdb, fmt.Sprintf("%s-%s-%d", vdb.Name, sc.GenCompatibleFQDN(), podIndex))
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
	return GenNamespacedName(vdb, fmt.Sprintf("%s-%s-%s-%d", vapi.LocalDataPVC, vdb.Name, sc.GenCompatibleFQDN(), podIndex))
}

// GenPVName returns the name of a dummy PV for test purposes
func GenPVName(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) types.NamespacedName {
	return types.NamespacedName{
		Name: fmt.Sprintf("pv-%s-%s-%s-%d", vapi.LocalDataPVC, vdb.Name, sc.GenCompatibleFQDN(), podIndex),
	}
}

// GetServerContainerIndex returns the correct server container
// index according to the NMA running mode. If there is no nma
// container, the server will be the first, otherwise the second
func GetServerContainerIndex(vdb *vapi.VerticaDB) int {
	if vdb.IsNMASideCarDeploymentEnabled() {
		return serverContainerIndex
	}
	return firstContainerIndex
}

// GetServerContainerIndexInContainers returns the server index in a given
// containers slice coming from a Vertica pod
func GetServerContainerIndexInSlice(cnts []corev1.Container) int {
	// the server can either be the first or the 2nd container(if nma sidecar is enabled)
	if cnts[firstContainerIndex].Name == ServerContainer {
		return firstContainerIndex
	}
	return serverContainerIndex
}

// GetNMAContainerIndex returns the index of
// the NMA container
func GetNMAContainerIndex() int {
	// nma when in a sidecar is always the first container
	return firstContainerIndex
}

func GetFirstContainerIndex() int {
	return firstContainerIndex
}
