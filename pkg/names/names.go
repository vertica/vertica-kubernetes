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

package names

import (
	"fmt"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

// GenExtSvcName returns the name of the external service object.
func GenExtSvcName(vdb *vapi.VerticaDB, sc *vapi.Subcluster) types.NamespacedName {
	return types.NamespacedName{
		Name:      vdb.Name + "-" + sc.Name,
		Namespace: vdb.Namespace,
	}
}

// GenHlSvcName returns the name of the headless service object.
func GenHlSvcName(vdb *vapi.VerticaDB) types.NamespacedName {
	return types.NamespacedName{
		Name:      vdb.Name,
		Namespace: vdb.Namespace,
	}
}

// GenStsName returns the name of the statefulset object
func GenStsName(vdb *vapi.VerticaDB, sc *vapi.Subcluster) types.NamespacedName {
	return types.NamespacedName{
		Name:      vdb.Name + "-" + sc.Name,
		Namespace: vdb.Namespace,
	}
}

// GenCommunalCredSecretName returns the name of the secret that has the credentials to access s3
func GenCommunalCredSecretName(vdb *vapi.VerticaDB) types.NamespacedName {
	return types.NamespacedName{
		Name:      vdb.Spec.Communal.CredentialSecret,
		Namespace: vdb.Namespace,
	}
}

// GenSUPasswdSecretName returns the name of the secret that has the superuser password
func GenSUPasswdSecretName(vdb *vapi.VerticaDB) types.NamespacedName {
	return types.NamespacedName{
		Name:      vdb.Spec.SuperuserPasswordSecret,
		Namespace: vdb.Namespace,
	}
}

// GenPodName returns the name of a specific pod in a subcluster
// The name of the pod is generated, this function is just a helper for when we need
// to lookup a pod by its generated name.
func GenPodName(vdb *vapi.VerticaDB, sc *vapi.Subcluster, podIndex int32) types.NamespacedName {
	return types.NamespacedName{
		Name:      vdb.Name + "-" + sc.Name + "-" + fmt.Sprintf("%d", podIndex),
		Namespace: vdb.Namespace,
	}
}
