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

package vdb

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ServerContainer      = "server"
	ServerContainerIndex = 0
)

type ObjReconcileModeType uint8

const (
	// Must maintain the same size when reconciling statefulsets
	ObjReconcileModePreserveScaling = 1 << iota
	// Must maintain the same delete policy when reconciling statefulsets
	ObjReconcileModePreserveUpdateStrategy
	// Reconcile to consider every change. Without this we will skip svc objects.
	ObjReconcileModeAll
)

// ObjReconciler will reconcile for all dependent Kubernetes objects. This is
// used for a single reconcile iteration.
type ObjReconciler struct {
	VRec   *VerticaDBReconciler
	Log    logr.Logger
	Vdb    *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts *PodFacts
	Mode   ObjReconcileModeType
}

// MakeObjReconciler will build an ObjReconciler object
func MakeObjReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, pfacts *PodFacts,
	mode ObjReconcileModeType) controllers.ReconcileActor {
	return &ObjReconciler{
		VRec:   vdbrecon,
		Log:    log.WithName("ObjReconciler"),
		Vdb:    vdb,
		PFacts: pfacts,
		Mode:   mode,
	}
}

// Reconcile is the main driver for reconciliation of Kubernetes objects.
// This will ensure the desired svc and sts objects exist and are in the correct state.
func (o *ObjReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if err := o.PFacts.Collect(ctx, o.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure any secrets/configMaps that we mount exist with the correct keys.
	// We catch the errors here so that we can provide timely events.
	if res, err := o.checkMountedObjs(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// We create a single headless service for the entire cluster.  Check to make sure that exists.
	if err := o.reconcileHlSvc(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// Check the objects for subclusters that should exist.  This will create
	// missing objects and update existing objects to match the vdb.
	if res, err := o.checkForCreatedSubclusters(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Check to see if we need to remove any objects for deleted subclusters
	if res, err := o.checkForDeletedSubcluster(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{}, nil
}

// checkMountedObjs will check if the mounted secrets/configMap exist and have
// the correct keys in them.
func (o *ObjReconciler) checkMountedObjs(ctx context.Context) (ctrl.Result, error) {
	// First check for secrets/configMaps that just need to exist.  We mount the
	// entire object in a directory and don't care what keys it has.
	if o.Vdb.Spec.LicenseSecret != "" {
		_, res, err := getSecret(ctx, o.VRec, o.Vdb,
			names.GenNamespacedName(o.Vdb, o.Vdb.Spec.LicenseSecret))
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	if o.Vdb.Spec.Communal.HadoopConfig != "" {
		_, res, err := getConfigMap(ctx, o.VRec, o.Vdb,
			names.GenNamespacedName(o.Vdb, o.Vdb.Spec.Communal.HadoopConfig))
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	// Skip if HTTP server is explicitly disabled. For auto, some of the work
	// isn't needed here. But we don't know the version, so we assume we need
	// it.
	if !o.Vdb.IsHTTPServerDisabled() {
		// When the HTTP server is enabled, a secret must exist that has the
		// certs to use for it.  There is a reconciler that is run before this
		// that will create the secret.  We will requeue if we find the Vdb
		// doesn't have the secret set.
		if o.Vdb.Spec.HTTPServerTLSSecret == "" {
			o.VRec.Event(o.Vdb, corev1.EventTypeWarning, events.HTTPServerNotSetup,
				"The httpServerTLSSecret must be set when Vertica's http server is enabled")
			return ctrl.Result{Requeue: true}, nil
		}
		_, res, err := getSecret(ctx, o.VRec, o.Vdb,
			names.GenNamespacedName(o.Vdb, o.Vdb.Spec.HTTPServerTLSSecret))
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	// Next check for secrets that must have specific keys.

	if o.Vdb.Spec.KerberosSecret != "" {
		keyNames := []string{filepath.Base(paths.Krb5Conf), filepath.Base(paths.Krb5Keytab)}
		if res, err := o.checkSecretHasKeys(ctx, "Kerberos", o.Vdb.Spec.KerberosSecret, keyNames); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	if o.Vdb.Spec.SSHSecret != "" {
		if res, err := o.checkSecretHasKeys(ctx, "SSH", o.Vdb.Spec.SSHSecret, paths.SSHKeyPaths); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	if o.Vdb.Spec.HTTPServerTLSSecret != "" {
		keyNames := []string{corev1.TLSPrivateKeyKey, corev1.TLSCertKey, paths.HTTPServerCACrtName}
		if res, err := o.checkSecretHasKeys(ctx, "HTTPServer", o.Vdb.Spec.HTTPServerTLSSecret, keyNames); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// checkSecretHasKeys is a helper to check that a secret has a set of keys in it
func (o *ObjReconciler) checkSecretHasKeys(ctx context.Context, secretType, secretName string, keyNames []string) (ctrl.Result, error) {
	secret, res, err := getSecret(ctx, o.VRec, o.Vdb, names.GenNamespacedName(o.Vdb, secretName))
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	for _, key := range keyNames {
		if _, ok := secret.Data[key]; !ok {
			o.VRec.Eventf(o.Vdb, corev1.EventTypeWarning, events.MissingSecretKeys,
				"%s secret '%s' has missing key '%s'", secretType, secretName, key)
			return ctrl.Result{Requeue: true}, nil
		}
	}
	return ctrl.Result{}, nil
}

// checkForCreatedSubclusters handles reconciliation of subclusters that should exist
func (o *ObjReconciler) checkForCreatedSubclusters(ctx context.Context) (ctrl.Result, error) {
	processedExtSvc := map[string]bool{} // Keeps track of service names we have reconciled
	for i := range o.Vdb.Spec.Subclusters {
		sc := &o.Vdb.Spec.Subclusters[i]
		// Transient subclusters never have their own service objects.  They always
		// reuse ones we have for other primary/secondary subclusters.
		if !sc.IsTransient {
			// Multiple subclusters may share the same service name. Only
			// reconcile for the first subcluster.
			svcName := names.GenExtSvcName(o.Vdb, sc)
			_, ok := processedExtSvc[svcName.Name]
			if !ok {
				expSvc := builder.BuildExtSvc(svcName, o.Vdb, sc, builder.MakeSvcSelectorLabelsForServiceNameRouting)
				if err := o.reconcileExtSvc(ctx, expSvc, sc); err != nil {
					return ctrl.Result{}, err
				}
				processedExtSvc[svcName.Name] = true
			}
		}

		if res, err := o.reconcileSts(ctx, sc); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// checkForDeletedSubcluster will remove any objects that were created for
// subclusters that don't exist anymore.
func (o *ObjReconciler) checkForDeletedSubcluster(ctx context.Context) (ctrl.Result, error) {
	if o.Mode&ObjReconcileModePreserveScaling != 0 {
		// Bypass this check since we won't be doing any scale down with this reconcile
		return ctrl.Result{}, nil
	}

	finder := iter.MakeSubclusterFinder(o.VRec.Client, o.Vdb)

	// Find any statefulsets that need to be deleted
	stss, err := finder.FindStatefulSets(ctx, iter.FindNotInVdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	for i := range stss.Items {
		// Ensure that we have correctly done db_remove_node and uninstall for
		// all pods in the subcluster.  If that isn't the case, we requeue to
		// give those reconcilers a chance to do those actions.
		if r, e := o.checkIfReadyForStsUpdate(0, &stss.Items[i]); verrors.IsReconcileAborted(r, e) {
			return r, e
		}

		err = o.VRec.Client.Delete(ctx, &stss.Items[i])
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Find any service objects that need to be deleted
	svcs, err := finder.FindServices(ctx, iter.FindNotInVdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	for i := range svcs.Items {
		err = o.VRec.Client.Delete(ctx, &svcs.Items[i])
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileExtSvc verifies the external service objects exists and creates it if necessary.
func (o ObjReconciler) reconcileExtSvc(ctx context.Context, expSvc *corev1.Service, sc *vapi.Subcluster) error {
	svcName := types.NamespacedName{Name: expSvc.Name, Namespace: expSvc.Namespace}
	return o.reconcileSvc(ctx, expSvc, svcName, sc, o.reconcileExtSvcFields)
}

// reconcileHlSvc verifies the headless service object exists and creates it if necessary.
func (o ObjReconciler) reconcileHlSvc(ctx context.Context) error {
	svcName := names.GenHlSvcName(o.Vdb)
	expSvc := builder.BuildHlSvc(svcName, o.Vdb)
	return o.reconcileSvc(ctx, expSvc, svcName, nil, o.reconcileHlSvcFields)
}

// reconcileSvc verifies the service object exists and creates it if necessary.
func (o ObjReconciler) reconcileSvc(ctx context.Context, expSvc *corev1.Service, svcName types.NamespacedName,
	sc *vapi.Subcluster, reconcileFieldsFunc func(*corev1.Service, *corev1.Service, *vapi.Subcluster) *corev1.Service) error {
	if o.Mode&ObjReconcileModeAll == 0 {
		// Bypass this check since we are doing changes to statefulsets only
		return nil
	}

	curSvc := &corev1.Service{}
	err := o.VRec.Client.Get(ctx, svcName, curSvc)
	if err != nil && errors.IsNotFound(err) {
		return o.createService(ctx, expSvc, svcName)
	}

	newSvc := reconcileFieldsFunc(curSvc, expSvc, sc)

	if newSvc != nil {
		o.Log.Info("updating svc", "Name", svcName)
		return o.VRec.Client.Update(ctx, newSvc)
	}
	return nil
}

// reconcileExtSvcFields merges relevant expSvc fields into curSvc, and
// returns an updated curSvc if one or more fields changed. Returns nil
// if nothing changed.
func (o ObjReconciler) reconcileExtSvcFields(curSvc, expSvc *corev1.Service, sc *vapi.Subcluster) *corev1.Service {
	updated := false

	if stringMapDiffer(expSvc.ObjectMeta.Annotations, curSvc.ObjectMeta.Annotations) {
		updated = true
		curSvc.ObjectMeta.Annotations = expSvc.ObjectMeta.Annotations
	}

	// Update the svc according to fields that changed w.r.t  expSvc
	if expSvc.Spec.Type != curSvc.Spec.Type {
		updated = true
		curSvc.Spec.Type = expSvc.Spec.Type
	}

	if expSvc.Spec.Type == corev1.ServiceTypeLoadBalancer || expSvc.Spec.Type == corev1.ServiceTypeNodePort {
		// We only update the node port if one was specified by the user and it
		// differs from what is currently in use. Otherwise, they must stay the
		// same. This protects us from changing the k8s generated node port each
		// time there is a Service object update.
		explicitNodePortByIndex := []int32{sc.NodePort, sc.VerticaHTTPNodePort}
		for i := range curSvc.Spec.Ports {
			if explicitNodePortByIndex[i] != 0 {
				if expSvc.Spec.Ports[i].NodePort != curSvc.Spec.Ports[i].NodePort {
					updated = true
					curSvc.Spec.Ports[i] = expSvc.Spec.Ports[i]
				}
			}
		}
	} else {
		// Ensure the nodePort is cleared for each port we expose.  That setting
		// is only valid for LoadBalancer and NodePort service types.
		for i := range curSvc.Spec.Ports {
			if curSvc.Spec.Ports[i].NodePort != 0 {
				updated = true
				curSvc.Spec.Ports[i].NodePort = 0
			}
		}
	}

	if !reflect.DeepEqual(expSvc.Spec.ExternalIPs, curSvc.Spec.ExternalIPs) {
		updated = true
		curSvc.Spec.ExternalIPs = expSvc.Spec.ExternalIPs
	}

	if expSvc.Spec.LoadBalancerIP != curSvc.Spec.LoadBalancerIP {
		updated = true
		curSvc.Spec.LoadBalancerIP = expSvc.Spec.LoadBalancerIP
	}

	// Check if the selectors are changing
	if stringMapDiffer(expSvc.Spec.Selector, curSvc.Spec.Selector) {
		curSvc.Spec.Selector = expSvc.Spec.Selector
		updated = true
	}

	if stringMapDiffer(expSvc.Labels, curSvc.Labels) {
		updated = true
		curSvc.Labels = expSvc.Labels
	}

	if updated {
		return curSvc
	}
	return nil
}

// stringMapDiffer will return true if the two maps are different. false means
// they are the same.
func stringMapDiffer(exp, cur map[string]string) bool {
	// The len() check is needed to compare against an empty map and a nil map.
	// We treat them the same for purpose of this comparison, but they are
	// different when comparing reflect.DeepEqual.
	if len(exp) == 0 && len(cur) == 0 {
		return false
	}
	return !reflect.DeepEqual(exp, cur)
}

// reconcileHlSvcFields merges relevant service fields into curSvc. This assumes
// we are reconciling the headless service object.
func (o ObjReconciler) reconcileHlSvcFields(curSvc, expSvc *corev1.Service, _ *vapi.Subcluster) *corev1.Service {
	if !reflect.DeepEqual(expSvc.Labels, curSvc.Labels) {
		curSvc.Labels = expSvc.Labels
		return curSvc
	}
	return nil
}

// createService creates a service.
func (o *ObjReconciler) createService(ctx context.Context, svc *corev1.Service, svcName types.NamespacedName) error {
	o.Log.Info("Creating service object", "Name", svcName)
	err := ctrl.SetControllerReference(o.Vdb, svc, o.VRec.Scheme)
	if err != nil {
		return err
	}
	return o.VRec.Client.Create(ctx, svc)
}

// reconcileSts reconciles the statefulset for a particular subcluster.  Returns
// true if any create/update was done.
func (o *ObjReconciler) reconcileSts(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	nm := names.GenStsName(o.Vdb, sc)
	curSts := &appsv1.StatefulSet{}
	expSts := builder.BuildStsSpec(nm, o.Vdb, sc, &o.VRec.DeploymentNames)
	err := o.VRec.Client.Get(ctx, nm, curSts)
	if err != nil && errors.IsNotFound(err) {
		o.Log.Info("Creating statefulset", "Name", nm, "Size", expSts.Spec.Replicas, "Image", expSts.Spec.Template.Spec.Containers[0].Image)
		err = ctrl.SetControllerReference(o.Vdb, expSts, o.VRec.Scheme)
		if err != nil {
			return ctrl.Result{}, err
		}
		// Invalidate the pod facts cache since we are creating a new sts
		o.PFacts.Invalidate()
		return ctrl.Result{}, o.VRec.Client.Create(ctx, expSts)
	}

	// We can only remove pods if we have called remove node and done the
	// uninstall.  If we haven't yet done that we will requeue the
	// reconciliation.  This will cause us to go through the remove node and
	// uninstall reconcile actors to properly handle the scale down.
	if o.Mode&ObjReconcileModePreserveScaling == 0 {
		if r, e := o.checkIfReadyForStsUpdate(sc.Size, curSts); verrors.IsReconcileAborted(r, e) {
			return r, e
		}
	}

	// We always preserve the image. This is done because during upgrade, the
	// image is changed outside of this reconciler. It is done through a
	// separate update to the sts.
	i := names.ServerContainerIndex
	expSts.Spec.Template.Spec.Containers[i].Image = curSts.Spec.Template.Spec.Containers[i].Image

	// Preserve scaling if told to do so. This is used when doing early
	// reconciliation so that we have any necessary pods started.
	if o.Mode&ObjReconcileModePreserveScaling != 0 {
		expSts.Spec.Replicas = curSts.Spec.Replicas
	}
	// Preserve the delete policy as they may be changed temporarily by upgrade,
	// which we may be in the middle of.
	if o.Mode&ObjReconcileModePreserveUpdateStrategy != 0 {
		expSts.Spec.UpdateStrategy.Type = curSts.Spec.UpdateStrategy.Type
	}

	// We allow the requestSize to change in the VerticaDB.  But we cannot
	// propagate that in the sts spec.  We handle that by modifying the PVC in a
	// separate reconciler.  Reset the volume claim spec so that we don't try to
	// change it here.
	expSts.Spec.VolumeClaimTemplates = curSts.Spec.VolumeClaimTemplates

	// Update the sts by patching in fields that changed according to expSts.
	// Due to the omission of default fields in expSts, curSts != expSts.  We
	// always send a patch request, then compare what came back against origSts
	// to see if any change was done.
	patch := client.MergeFrom(curSts.DeepCopy())
	origSts := &appsv1.StatefulSet{}
	curSts.DeepCopyInto(origSts)
	expSts.Spec.DeepCopyInto(&curSts.Spec)
	curSts.Labels = expSts.Labels
	if err := o.VRec.Client.Patch(ctx, curSts, patch); err != nil {
		return ctrl.Result{}, err
	}
	if !reflect.DeepEqual(curSts.Spec, origSts.Spec) {
		o.Log.Info("Patching statefulset", "Name", expSts.Name, "Image", expSts.Spec.Template.Spec.Containers[0].Image)
		// Invalidate the pod facts cache since we are about to change the sts
		o.PFacts.Invalidate()
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}

// checkIfReadyForStsUpdate will check whether it is okay to proceed
// with the statefulset update.  This checks if we are deleting pods/sts and if
// what we are deleting has had proper cleanup. In the case of admintools, failure to
// do this will cause us to orphan entries leading admintools to fail for most
// operations.
func (o *ObjReconciler) checkIfReadyForStsUpdate(newStsSize int32, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	if newStsSize >= *sts.Spec.Replicas {
		// Nothing to do as we aren't scaling down.
		return ctrl.Result{}, nil
	}

	// Cycle through each pod that is going to be deleted to see if it is safe
	// to remove it.
	for i := newStsSize; i < *sts.Spec.Replicas; i++ {
		pn := names.GenPodNameFromSts(o.Vdb, sts, i)
		pf, ok := o.PFacts.Detail[pn]
		if !ok {
			return ctrl.Result{}, fmt.Errorf("could not find pod facts for pod '%s'", pn)
		}
		if pf.isInstalled || pf.dbExists {
			o.Log.Info("Requeue since some pods still need db_remove_node and uninstall done.",
				"name", pn, "isInstalled", pf.isInstalled, "dbExists", pf.dbExists)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}
