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
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ServerContainer      = "server"
	ServerContainerIndex = 0
	LocalDataPVC         = "local-data"
)

// ObjReconciler will reconcile for all dependent Kubernetes objects. This is
// used for a single reconcile iteration.
type ObjReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	Vdb    *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts *PodFacts
}

// MakeObjReconciler will build an ObjReconciler object
func MakeObjReconciler(cli client.Client, scheme *runtime.Scheme, log logr.Logger, vdb *vapi.VerticaDB, pfacts *PodFacts) ReconcileActor {
	return &ObjReconciler{Client: cli, Scheme: scheme, Log: log, Vdb: vdb, PFacts: pfacts}
}

func (o *ObjReconciler) GetClient() client.Client {
	return o.Client
}

func (o *ObjReconciler) GetVDB() *vapi.VerticaDB {
	return o.Vdb
}

func (o *ObjReconciler) CollectPFacts(ctx context.Context) error {
	return o.PFacts.Collect(ctx, o.Vdb)
}

// Reconcile is the main driver for reconciliation of Kubernetes objects.
// This will ensure the desired svc and sts objects exist and are in the correct state.
func (o *ObjReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
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

// checkForCreatedSubcluster handles reconciliation of one subcluster that should exist
func (o *ObjReconciler) checkForCreatedSubcluster(ctx context.Context, sc *vapi.Subcluster) error {
	if err := o.reconcileExtSvc(ctx, sc); err != nil {
		return err
	}

	return o.reconcileSts(ctx, sc)
}

// checkForDeletedSubcluster will remove any objects that were created for
// subclusters that don't exist anymore.
func (o *ObjReconciler) checkForDeletedSubcluster(ctx context.Context) error {
	finder := MakeSubclusterFinder(o.Client, o.Vdb)

	// Find any statefulsets that need to be delete
	stss, err := finder.FindStatefulSets(ctx, FindNotInVdb)
	if err != nil {
		return err
	}

	for i := range stss.Items {
		err = o.Client.Delete(ctx, &stss.Items[i])
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
		err = o.Client.Delete(ctx, &svcs.Items[i])
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
	err := o.Client.Get(ctx, svcName, curSvc)
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
		return o.Client.Update(ctx, curSvc)
	}
	return nil
}

// reconcileHlSvc verifies the headless service object exists and creates it if necessary.
func (o ObjReconciler) reconcileHlSvc(ctx context.Context) error {
	curSvc := &corev1.Service{}
	svcName := names.GenHlSvcName(o.Vdb)
	expSvc := buildHlSvc(svcName, o.Vdb)
	err := o.Client.Get(ctx, svcName, curSvc)
	if err != nil && errors.IsNotFound(err) {
		return o.createService(ctx, expSvc, svcName)
	}
	return nil
}

// createService creates a service.
func (o *ObjReconciler) createService(ctx context.Context, svc *corev1.Service, svcName types.NamespacedName) error {
	o.Log.Info("Creating service object", "Name", svcName)
	err := ctrl.SetControllerReference(o.Vdb, svc, o.Scheme)
	if err != nil {
		return err
	}
	return o.Client.Create(ctx, svc)
}

// reconcileSts reconciles the statefulset for a particular subcluster.
func (o *ObjReconciler) reconcileSts(ctx context.Context, sc *vapi.Subcluster) error {
	nm := names.GenStsName(o.Vdb, sc)
	curSts := &appsv1.StatefulSet{}
	expSts := buildStsSpec(nm, o.Vdb, sc)
	err := o.Client.Get(ctx, nm, curSts)
	if err != nil && errors.IsNotFound(err) {
		o.Log.Info("Creating statefulset", "Name", nm, "Size", expSts.Spec.Replicas)
		err = ctrl.SetControllerReference(o.Vdb, expSts, o.Scheme)
		if err != nil {
			return err
		}
		// Invalidate the pod facts cache since we are creating a new sts
		o.PFacts.Invalidate()
		return o.Client.Create(ctx, expSts)
	}

	// Update the sts by patching in fields that changed according to expSts
	if !reflect.DeepEqual(expSts.Spec, curSts.Spec) {
		patch := client.MergeFrom(curSts.DeepCopy())
		expSts.Spec.DeepCopyInto(&curSts.Spec)
		// Invalidate the pod facts cache since we are about to change the sts
		o.PFacts.Invalidate()
		return o.Client.Patch(ctx, curSts, patch)
	}
	return nil
}
