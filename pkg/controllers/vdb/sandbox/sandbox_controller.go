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

package sandbox

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	verticaDBNameKey = "verticaDBName"
)

// SandboxConfigMapReconciler reconciles a ConfigMap for sandboxing
type SandboxConfigMapReconciler struct {
	client.Client
	Log         logr.Logger
	Scheme      *runtime.Scheme
	Cfg         *rest.Config
	EVRec       record.EventRecorder
	Concurrency int
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=vertica.com,resources=verticadbs,verbs=get;list;watch

func (r *SandboxConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&corev1.ConfigMap{},
			builder.WithPredicates(r.predicateFuncs(), predicate.ResourceVersionChangedPredicate{}),
		).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.Concurrency}).
		Complete(r)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ConfigMap object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *SandboxConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("sandboxConfigMap", req.NamespacedName)
	log.Info("starting reconcile of sandbox configmap")

	// Fetch the ConfigMap instance
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, req.NamespacedName, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// ConfigMap not found, return and don't requeue
			log.Info("ConfigMap not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get ConfigMap")
		return ctrl.Result{}, err
	}

	// Fetch the VDB found in the configmap
	vdb := &v1.VerticaDB{}
	nm := names.GenNamespacedName(configMap, configMap.Data[verticaDBNameKey])
	var res ctrl.Result
	if res, err = vk8s.FetchVDB(ctx, r, configMap, nm, vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Iterate over each actor
	actors := r.constructActors(vdb, log)
	for _, act := range actors {
		log.Info("starting actor", "name", fmt.Sprintf("%T", act))
		res, err = act.Reconcile(ctx, &req)
		// Error or a request to requeue will stop the reconciliation.
		if verrors.IsReconcileAborted(res, err) {
			log.Info("aborting reconcile of Sandbox ConfigMap", "result", res, "err", err)
			return res, err
		}
	}
	log.Info("ending reconcile of Sandbox ConfigMap", "result", res, "err", err)
	return ctrl.Result{}, nil
}

// constructActors will a list of actors that should be run for the reconcile.
// Order matters in that some actors depend on the successeful execution of
// earlier ones.
func (r *SandboxConfigMapReconciler) constructActors(_ *v1.VerticaDB,
	_ logr.Logger) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile a sandbox configmap.
	return nil
}

// predicateFuncs returns a predicate that will be used to filter
// create/update/delete events based on specific labels
func (r *SandboxConfigMapReconciler) predicateFuncs() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// filter based on labels
			return r.containsSandboxConfigMapLabels(e.Object.GetLabels())
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			// filter based on labels
			return r.containsSandboxConfigMapLabels(e.ObjectNew.GetLabels())
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// filter based on labels
			return r.containsSandboxConfigMapLabels(e.Object.GetLabels())
		},
	}
}

// containsSandboxConfigMapLabels returns true if the labels map contains
// all sandbox configmap labels
func (r *SandboxConfigMapReconciler) containsSandboxConfigMapLabels(labels map[string]string) bool {
	for _, label := range vmeta.SandboxConfigMapLabels {
		_, ok := labels[label]
		if !ok {
			return false
		}
	}
	return true
}

// Event a wrapper for Event() that also writes a log entry
func (r *SandboxConfigMapReconciler) Event(obj runtime.Object, eventtype, reason, message string) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Event(obj, eventtype, reason, message)
}

// GetClient gives access to the Kubernetes client
func (r *SandboxConfigMapReconciler) GetClient() client.Client {
	return r.Client
}

// Eventf is a wrapper for Eventf() that also writes a log entry
func (r *SandboxConfigMapReconciler) Eventf(obj runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Eventf(obj, eventtype, reason, messageFmt, args...)
}
