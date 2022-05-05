/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package vdb

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PodAnnotationReconciler will maintain annotations in the pod about the running system
type PodAnnotationReconciler struct {
	VRec   *VerticaDBReconciler
	Vdb    *vapi.VerticaDB
	PFacts *PodFacts
}

// MakePodAnnotationReconciler will build a PodAnnotationReconciler object
func MakePodAnnotationReconciler(vdbrecon *VerticaDBReconciler,
	vdb *vapi.VerticaDB, pfacts *PodFacts) controllers.ReconcileActor {
	return &PodAnnotationReconciler{VRec: vdbrecon, Vdb: vdb, PFacts: pfacts}
}

// Reconcile will add annotations to each of the pods so that we flow down
// system information with the downwardAPI.  The intent of this additional data
// is for inclusion in Vertica data collector (DC) tables.
func (s *PodAnnotationReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Build up a list of the annotations that we will apply to each running pod.
	annotations, err := s.generateAnnotations()
	if err != nil {
		return ctrl.Result{}, err
	}

	// Iterate over pod that exists.
	for pn, pf := range s.PFacts.Detail {
		if pf.exists {
			if err := s.applyAnnotations(ctx, pn, annotations); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

// generateAnnotations will generate static annotations that will be applied to each running pod
func (s *PodAnnotationReconciler) generateAnnotations() (map[string]string, error) {
	// We get the k8s server information from the client.  It would better to
	// get the node the pod was assigned and fetch the system info from the
	// node.  This will give us more details information like what container
	// runtime they are using and versions for kubelet and kube-proxy.  However,
	// we would need an rbac rule to fetch the node.  And this rule would need
	// to be cluster scoped.  Currently, all of the rules for the operator are
	// namespaced scoped.  So it would make it harder to set those up -- naming
	// collisions of the cluster roles/rolebindings, harder to deploy with a
	// predefined service account.  Getting the server from the client is good
	// enough, as it doesn't require any new rbac rules.
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(s.VRec.Cfg)
	if err != nil {
		return nil, err
	}
	ver, err := discoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}
	s.VRec.Log.Info("Kubernetes server version", "version", ver.GitVersion, "gitCommit", ver.GitCommit, "buildDate", ver.BuildDate)

	return map[string]string{
		builder.KubernetesVersionAnnotation:   ver.GitVersion,
		builder.KubernetesGitCommitAnnotation: ver.GitCommit,
		builder.KubernetesBuildDateAnnotation: ver.BuildDate,
	}, nil
}

// applyAnnotations will ensure the annotations passed in are set for the given pod
func (s *PodAnnotationReconciler) applyAnnotations(ctx context.Context, podName types.NamespacedName, anns map[string]string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pod := &corev1.Pod{}
		if err := s.VRec.Client.Get(ctx, podName, pod); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		annotationsChanged := false
		for k, v := range anns {
			if pod.Annotations[k] != v {
				if pod.Annotations == nil {
					pod.Annotations = map[string]string{}
				}
				pod.Annotations[k] = v
				annotationsChanged = true
			}
		}
		if annotationsChanged {
			err := s.VRec.Client.Update(ctx, pod)
			if err == nil {
				// We have added/updated the annotations.  Refresh the podfacts.
				// This saves having to invalidate the entire thing.
				s.PFacts.Detail[podName].hasDCTableAnnotations = true
			}
			return err
		}
		return nil
	})
}
