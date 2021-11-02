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
	"context"
	"reflect"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
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

// ObjReconciler will reconcile for all dependent Kubernetes objects. This is
// used for a single reconcile iteration.
type ObjReconciler struct {
	VRec              *VerticaDBReconciler
	Log               logr.Logger
	Vdb               *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts            *PodFacts
	PatchImageAllowed bool // a patch can only change the image when this is set to true
}

// MakeObjReconciler will build an ObjReconciler object
func MakeObjReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, pfacts *PodFacts) ReconcileActor {
	return &ObjReconciler{
		VRec:   vdbrecon,
		Log:    log,
		Vdb:    vdb,
		PFacts: pfacts}
}

// Reconcile is the main driver for reconciliation of Kubernetes objects.
// This will ensure the desired svc and sts objects exist and are in the correct state.
func (o *ObjReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// Ensure any secrets/configMaps that we mount exist with the correct keys.
	// We catch the errors here so that we can provide timely events.
	if res, err := o.checkMountedObjs(ctx); res.Requeue || err != nil {
		return res, err
	}

	// We create a single headless service for the entire cluster.  Check to make sure that exists.
	if err := o.reconcileHlSvc(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// Check the objects for subclusters that should exist.  This will create
	// missing objects and update existing objects to match the vdb.
	for i := range o.Vdb.Spec.Subclusters {
		if err := o.checkForCreatedSubcluster(ctx, &o.Vdb.Spec.Subclusters[i]); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Check to see if we need to remove any objects for deleted subclusters
	if err := o.checkForDeletedSubcluster(ctx); err != nil {
		return ctrl.Result{}, err
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
		if res.Requeue || err != nil {
			return res, err
		}
	}

	if o.Vdb.Spec.Communal.HadoopConfig != "" {
		_, res, err := getConfigMap(ctx, o.VRec, o.Vdb,
			names.GenNamespacedName(o.Vdb, o.Vdb.Spec.Communal.HadoopConfig))
		if res.Requeue || err != nil {
			return res, err
		}
	}

	// Next check for secrets that must have specific keys.

	if o.Vdb.Spec.KerberosSecret != "" {
		secret, res, err := getSecret(ctx, o.VRec, o.Vdb, names.GenNamespacedName(o.Vdb, o.Vdb.Spec.KerberosSecret))
		if res.Requeue || err != nil {
			return res, err
		}

		keyNames := []string{paths.Krb5Conf, paths.Krb5Keytab}
		for _, key := range keyNames {
			if _, ok := secret.Data[key]; !ok {
				o.VRec.EVRec.Eventf(o.Vdb, corev1.EventTypeWarning, events.MissingSecretKeys,
					"Kerberos secret '%s' has missing key '%s'", o.Vdb.Spec.KerberosSecret, key)
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

// checkForCreatedSubcluster handles reconciliation of one subcluster that should exist
func (o *ObjReconciler) checkForCreatedSubcluster(ctx context.Context, sc *vapi.Subcluster) error {
	if err := o.reconcileExtSvc(ctx, sc); err != nil {
		return err
	}

	_, err := o.reconcileSts(ctx, sc)
	return err
}

// checkForDeletedSubcluster will remove any objects that were created for
// subclusters that don't exist anymore.
func (o *ObjReconciler) checkForDeletedSubcluster(ctx context.Context) error {
	finder := MakeSubclusterFinder(o.VRec.Client, o.Vdb)

	// Find any statefulsets that need to be delete
	stss, err := finder.FindStatefulSets(ctx, FindNotInVdb)
	if err != nil {
		return err
	}

	for i := range stss.Items {
		err = o.VRec.Client.Delete(ctx, &stss.Items[i])
		if err != nil {
			return err
		}
	}

	// Find any service objects that need to be deleted
	svcs, err := finder.FindServices(ctx, FindNotInVdb)
	if err != nil {
		return err
	}

	for i := range svcs.Items {
		err = o.VRec.Client.Delete(ctx, &svcs.Items[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// reconcileExtSvc verifies the external service objects exists and creates it if necessary.
func (o ObjReconciler) reconcileExtSvc(ctx context.Context, sc *vapi.Subcluster) error {
	curSvc := &corev1.Service{}
	svcName := names.GenExtSvcName(o.Vdb, sc)
	expSvc := buildExtSvc(svcName, o.Vdb, sc)
	err := o.VRec.Client.Get(ctx, svcName, curSvc)
	if err != nil && errors.IsNotFound(err) {
		return o.createService(ctx, expSvc, svcName)
	}
	updated := false
	const verticaPortIndex = 0
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
	if updated {
		o.Log.Info("updating svc", "Name", svcName)
		return o.VRec.Client.Update(ctx, curSvc)
	}
	return nil
}

// reconcileHlSvc verifies the headless service object exists and creates it if necessary.
func (o ObjReconciler) reconcileHlSvc(ctx context.Context) error {
	curSvc := &corev1.Service{}
	svcName := names.GenHlSvcName(o.Vdb)
	expSvc := buildHlSvc(svcName, o.Vdb)
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
func (o *ObjReconciler) reconcileSts(ctx context.Context, sc *vapi.Subcluster) (bool, error) {
	nm := names.GenStsName(o.Vdb, sc)
	curSts := &appsv1.StatefulSet{}
	expSts := buildStsSpec(nm, o.Vdb, sc)
	err := o.VRec.Client.Get(ctx, nm, curSts)
	if err != nil && errors.IsNotFound(err) {
		o.Log.Info("Creating statefulset", "Name", nm, "Size", expSts.Spec.Replicas, "Image", expSts.Spec.Template.Spec.Containers[0].Image)
		err = ctrl.SetControllerReference(o.Vdb, expSts, o.VRec.Scheme)
		if err != nil {
			return false, err
		}
		// Invalidate the pod facts cache since we are creating a new sts
		o.PFacts.Invalidate()
		return true, o.VRec.Client.Create(ctx, expSts)
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
		return false, err
	}
	if !reflect.DeepEqual(curSts.Spec, origSts.Spec) {
		o.Log.Info("Patching statefulset", "Name", expSts.Name, "Image", expSts.Spec.Template.Spec.Containers[0].Image)
		// Invalidate the pod facts cache since we are about to change the sts
		o.PFacts.Invalidate()
		return true, nil
	}
	return false, nil
}
