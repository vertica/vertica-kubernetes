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
	"github.com/google/uuid"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vdbcontroller "github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
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
		Watches(
			&source.Kind{Type: &appsv1.StatefulSet{}},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForStatesulSet),
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
	// Generate a unique uuid for each reconcile iteration so we can easily
	// trace actions within a reconcile.
	reconcileUUID := uuid.New()
	log := r.Log.WithValues("configMap", req.NamespacedName, "reconcile-uuid", reconcileUUID)
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

	// validate the configmap data field
	err = validateConfigMapData(configMap)
	if err != nil {
		log.Error(err, "invalid ConfigMap")
		return ctrl.Result{}, err
	}

	// Fetch the VDB found in the configmap
	vdb := &v1.VerticaDB{}
	nm := names.GenNamespacedName(configMap, configMap.Data[v1.VerticaDBNameKey])
	var res ctrl.Result
	if res, err = vk8s.FetchVDB(ctx, r, configMap, nm, vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	sandboxName := configMap.Data[v1.SandboxNameKey]
	log = log.WithValues("verticadb", vdb.Name, "sandbox", sandboxName)

	passwd, err := vk8s.GetSuperuserPassword(ctx, r.Client, log, r, vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	prunner := cmds.MakeClusterPodRunner(log, r.Cfg, vdb.GetVerticaUser(), passwd)
	pfacts := podfacts.MakePodFactsForSandbox(r, prunner, log, passwd, sandboxName)
	dispatcher := vadmin.MakeVClusterOps(log, vdb, r.Client, passwd, r.EVRec, vadmin.SetupVClusterOps)

	// Iterate over each actor
	actors := r.constructActors(vdb, log, prunner, &pfacts, dispatcher, configMap)
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
func (r *SandboxConfigMapReconciler) constructActors(vdb *v1.VerticaDB, log logr.Logger, prunner *cmds.ClusterPodRunner,
	pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher, configMap *corev1.ConfigMap) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile a sandbox configmap.
	return []controllers.ReconcileActor{
		// Ensure we support sandboxing and vclusterops
		MakeVerifyDeploymentReconciler(r, vdb, log),
		// Move the subclusters from a sandbox to the main cluster
		MakeUnsandboxSubclusterReconciler(r, vdb, log, r.Client, pfacts, dispatcher, configMap, prunner),
		// Update the vdb status for the sandbox nodes/pods
		vdbcontroller.MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Upgrade the sandbox using the offline method
		vdbcontroller.MakeOfflineUpgradeReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		// Add annotations/labels to each pod about the host running them
		vdbcontroller.MakeAnnotateAndLabelPodReconciler(r, log, vdb, pfacts),
		// Restart any down pods
		vdbcontroller.MakeRestartReconciler(r, log, vdb, prunner, pfacts, true, dispatcher),
	}
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
func (r *SandboxConfigMapReconciler) containsSandboxConfigMapLabels(labs map[string]string) bool {
	for _, label := range vmeta.SandboxConfigMapLabels {
		val, ok := labs[label]
		if !ok {
			return false
		}
		// some labels have a constant value
		// so we are checking if they have the expected values
		switch label {
		case vmeta.ManagedByLabel:
			if val != vmeta.OperatorName {
				return false
			}
		case vmeta.ComponentLabel:
			if val != vmeta.ComponentDatabase {
				return false
			}
		default:
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

// GetEventRecorder gives access to the event recorder
func (r *SandboxConfigMapReconciler) GetEventRecorder() record.EventRecorder {
	return r.EVRec
}

// Eventf is a wrapper for Eventf() that also writes a log entry
func (r *SandboxConfigMapReconciler) Eventf(obj runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Eventf(obj, eventtype, reason, messageFmt, args...)
}

// GetConfig gives access to *rest.Config
func (r *SandboxConfigMapReconciler) GetConfig() *rest.Config {
	return r.Cfg
}

// findObjectsForStatesulSet will generate requests to reconcile sandbox ConfigMaps
// based on watched Statefulset
func (r *SandboxConfigMapReconciler) findObjectsForStatesulSet(sts client.Object) []reconcile.Request {
	configMaps := corev1.ConfigMapList{}
	stsLabels := sts.GetLabels()
	sbLabels := make(map[string]string, len(vmeta.SandboxConfigMapLabels))
	for _, label := range vmeta.SandboxConfigMapLabels {
		sbLabels[label] = stsLabels[label]
	}
	listOps := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set(sbLabels)),
		Namespace:     sts.GetNamespace(),
	}
	err := r.List(context.Background(), &configMaps, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	vdbName := stsLabels[vmeta.VDBInstanceLabel]
	sandbox := stsLabels[vmeta.SandboxNameLabel]
	requests := []reconcile.Request{}
	for i := range configMaps.Items {
		// data within a ConfigMap is a map[string]string, and you typically
		// can't index it directly with the Kubernetes client-go FieldIndexer interface.
		// That's why we filter the configmaps to keep only the one that has the same
		// sandbox name and vdb name as the statefulset
		cm := &configMaps.Items[i]
		cmVdbName, vdbExists := cm.Data[v1.VerticaDBNameKey]
		cmSbName, sbExists := cm.Data[v1.SandboxNameKey]
		if (!vdbExists || !sbExists) ||
			cmVdbName != vdbName ||
			cmSbName != sandbox {
			continue
		}
		request := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      configMaps.Items[i].GetName(),
				Namespace: configMaps.Items[i].GetNamespace(),
			},
		}
		requests = append(requests, request)
	}
	return requests
}

// validateConfigMapData verifies that sandbox configmap contains
// a sandbox and a verticaDB
func validateConfigMapData(cm *corev1.ConfigMap) error {
	_, hasVDBName := cm.Data[v1.VerticaDBNameKey]
	sandbox, hasSandbox := cm.Data[v1.SandboxNameKey]
	if !hasVDBName || !hasSandbox {
		return fmt.Errorf("configmap data must contain '%q' and '%q'", v1.VerticaDBNameKey, v1.SandboxNameKey)
	}
	if sandbox == v1.MainCluster {
		return fmt.Errorf("configmap sandbox must be set")
	}
	return nil
}
