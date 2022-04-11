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

type ObjReconcileModeType string

const (
	// Consider all ways to reconcile - add, delete, modify k8s objects
	ObjReconcileModeAll ObjReconcileModeType = "All"
	// Only reconcile objects that are missing.
	ObjReconcileModeIfNotFound ObjReconcileModeType = "IfNotFound"
)

// ObjReconciler will reconcile for all dependent Kubernetes objects. This is
// used for a single reconcile iteration.
type ObjReconciler struct {
	VRec              *VerticaDBReconciler
	Log               logr.Logger
	Vdb               *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts            *PodFacts
	PatchImageAllowed bool // a patch can only change the image when this is set to true
	Mode              ObjReconcileModeType
}

// MakeObjReconciler will build an ObjReconciler object
func MakeObjReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, pfacts *PodFacts,
	mode ObjReconcileModeType) controllers.ReconcileActor {
	return &ObjReconciler{
		VRec:   vdbrecon,
		Log:    log,
		Vdb:    vdb,
		PFacts: pfacts,
		Mode:   mode,
	}
}

// Reconcile is the main driver for reconciliation of Kubernetes objects.
// This will ensure the desired svc and sts objects exist and are in the correct state.
func (o *ObjReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
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
	for i := range o.Vdb.Spec.Subclusters {
		if res, err := o.checkForCreatedSubcluster(ctx, &o.Vdb.Spec.Subclusters[i]); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
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
			o.VRec.EVRec.Eventf(o.Vdb, corev1.EventTypeWarning, events.MissingSecretKeys,
				"%s secret '%s' has missing key '%s'", secretType, secretName, key)
			return ctrl.Result{Requeue: true}, nil
		}
	}
	return ctrl.Result{}, nil
}

// checkForCreatedSubcluster handles reconciliation of one subcluster that should exist
func (o *ObjReconciler) checkForCreatedSubcluster(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	// Transient subclusters never have their own service objects.  They always
	// reuse ones we have for other primary/secondary subclusters.
	if !sc.IsTransient {
		svcName := names.GenExtSvcName(o.Vdb, sc)
		expSvc := builder.BuildExtSvc(svcName, o.Vdb, sc, builder.MakeSvcSelectorLabelsForServiceNameRouting)
		if err := o.reconcileExtSvc(ctx, expSvc, sc); err != nil {
			return ctrl.Result{}, err
		}
	}

	return o.reconcileSts(ctx, sc)
}

// checkForDeletedSubcluster will remove any objects that were created for
// subclusters that don't exist anymore.
func (o *ObjReconciler) checkForDeletedSubcluster(ctx context.Context) (ctrl.Result, error) {
	if o.Mode == ObjReconcileModeIfNotFound {
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
		// give those reconcilers a chance to do those actions.  Failure to do
		// this will result in corruption of admintools.conf.
		if r, e := o.checkForOrphanAdmintoolsConfEntries(0, &stss.Items[i]); verrors.IsReconcileAborted(r, e) {
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
	curSvc := &corev1.Service{}
	svcName := types.NamespacedName{Name: expSvc.Name, Namespace: expSvc.Namespace}
	err := o.VRec.Client.Get(ctx, svcName, curSvc)
	if err != nil && errors.IsNotFound(err) {
		return o.createService(ctx, expSvc, svcName)
	}
	// Early out if the mode is set such that we only create objects if they are
	// missing.  The rest of the logic will attempt to update an existing object.
	if o.Mode == ObjReconcileModeIfNotFound {
		return nil
	}

	newSvc := o.reconcileExtSvcFields(curSvc, expSvc, sc)

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
	const verticaPortIndex = 0

	if !reflect.DeepEqual(expSvc.ObjectMeta.Annotations, curSvc.ObjectMeta.Annotations) {
		updated = true
		curSvc.ObjectMeta.Annotations = expSvc.ObjectMeta.Annotations
	}

	// Update the svc according to fields that changed w.r.t  expSvc
	if expSvc.Spec.Type != curSvc.Spec.Type {
		updated = true
		curSvc.Spec.Type = expSvc.Spec.Type
	}

	if expSvc.Spec.Type == corev1.ServiceTypeLoadBalancer || expSvc.Spec.Type == corev1.ServiceTypeNodePort {
		if sc.NodePort != 0 {
			if expSvc.Spec.Ports[verticaPortIndex].NodePort != curSvc.Spec.Ports[verticaPortIndex].NodePort {
				updated = true
				curSvc.Spec.Ports = expSvc.Spec.Ports
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
	if !reflect.DeepEqual(expSvc.Spec.Selector, curSvc.Spec.Selector) {
		curSvc.Spec.Selector = expSvc.Spec.Selector
		updated = true
	}

	if updated {
		return curSvc
	}
	return nil
}

// reconcileHlSvc verifies the headless service object exists and creates it if necessary.
func (o ObjReconciler) reconcileHlSvc(ctx context.Context) error {
	curSvc := &corev1.Service{}
	svcName := names.GenHlSvcName(o.Vdb)
	expSvc := builder.BuildHlSvc(svcName, o.Vdb)
	err := o.VRec.Client.Get(ctx, svcName, curSvc)
	if err != nil && errors.IsNotFound(err) {
		return o.createService(ctx, expSvc, svcName)
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
	expSts := builder.BuildStsSpec(nm, o.Vdb, sc, o.VRec.ServiceAccountName)
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
	// The rest of the logic deals with updating an existing object.  We do an
	// early out if the mode is set such that we only create objects if they are
	// missing.
	if o.Mode == ObjReconcileModeIfNotFound {
		return ctrl.Result{}, nil
	}

	// We can only remove pods if we have called 'admintools -t db_remove_node'
	// and done the uninstall.  If we haven't yet done that we will requeue the
	// reconciliation.  This will cause us to go through the remove node and
	// uninstall reconcile actors to properly handle the scale down.
	if r, e := o.checkForOrphanAdmintoolsConfEntries(sc.Size, curSts); verrors.IsReconcileAborted(r, e) {
		return r, e
	}

	// To distinguish when this is called as part of the upgrade reconciler, we
	// will only change the image for a patch when instructed to do so.
	if !o.PatchImageAllowed {
		i := names.ServerContainerIndex
		expSts.Spec.Template.Spec.Containers[i].Image = curSts.Spec.Template.Spec.Containers[i].Image
	}

	// Update the sts by patching in fields that changed according to expSts.
	// Due to the omission of default fields in expSts, curSts != expSts.  We
	// always send a patch request, then compare what came back against origSts
	// to see if any change was done.
	patch := client.MergeFrom(curSts.DeepCopy())
	origSts := &appsv1.StatefulSet{}
	curSts.DeepCopyInto(origSts)
	expSts.Spec.DeepCopyInto(&curSts.Spec)
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

// checkForOrphanAdmintoolsConfEntries will check whether it is okay to proceed
// with the statefulset update.  This checks if we are deleting pods/sts and if
// what we are deleting has had proper cleanup in admintools.conf.  Failure to
// do this will cause us to orphan entries leading admintools to fail for most
// operations.
func (o *ObjReconciler) checkForOrphanAdmintoolsConfEntries(newStsSize int32, sts *appsv1.StatefulSet) (ctrl.Result, error) {
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
		if !pf.isInstalled.IsFalse() || !pf.dbExists.IsFalse() {
			o.Log.Info("Requeue since some pods still need db_remove_node and uninstall done.",
				"name", pn, "isInstalled", pf.isInstalled, "dbExists", pf.dbExists)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}
