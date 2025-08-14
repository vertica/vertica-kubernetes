/*
 (c) Copyright [2021-2025] Open Text.
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
	"reflect"

	"github.com/go-logr/logr"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ServiceMonitorReconciler reconciles the ServiceMonitor for a VerticaDB.
type ServiceMonitorReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

func MakeServiceMonitorReconciler(vdb *vapi.VerticaDB, vrec *VerticaDBReconciler, log logr.Logger) controllers.ReconcileActor {
	return &ServiceMonitorReconciler{
		VRec: vrec,
		Vdb:  vdb,
		Log:  log.WithName("ServiceMonitorReconciler"),
	}
}

func (s *ServiceMonitorReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if Prometheus is not enabled or the DB is not initialized.
	if !opcfg.IsPrometheusEnabled() || !s.Vdb.IsDBInitialized() {
		s.Log.Info("Prometheus is not enabled or DB is not initialized, skipping ServiceMonitor reconciliation")
		return ctrl.Result{}, nil
	}

	err := s.reconcileBasicAuth(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, s.reconcileServiceMonitor(ctx)
}

// reconcileBasicAuth creates the basic auth secret if it does not exist.
// The secret contaons the username and password to authenticate to the DB.
func (s *ServiceMonitorReconciler) reconcileBasicAuth(ctx context.Context) error {
	if s.Vdb.IsSetForTLS() {
		return nil
	}
	nm := names.GenBasicauthSecretName(s.Vdb)
	curSec := &corev1.Secret{}

	err := s.VRec.GetClient().Get(ctx, nm, curSec)
	if err != nil && kerrors.IsNotFound(err) {
		password := ""
		if s.Vdb.Spec.PasswordSecret != "" {
			password, err = vk8s.GetSuperuserPassword(ctx, s.VRec.Client, s.Log, s.VRec, s.Vdb)
			if err != nil {
				return err
			}
		}
		expSec := builder.BuildBasicAuthSecret(s.Vdb, nm.Name, s.Vdb.GetVerticaUser(), password)
		return s.VRec.GetClient().Create(ctx, expSec)
	}

	return err
}

// reconcileServiceMonitor creates or updates the ServiceMonitor.
func (s *ServiceMonitorReconciler) reconcileServiceMonitor(ctx context.Context) error {
	nm := names.GenSvcMonitorName(s.Vdb)
	curSvcMon := &monitoringv1.ServiceMonitor{}
	basicAuthNm := names.GenBasicauthSecretName(s.Vdb)
	expSvcMon := builder.BuildServiceMonitor(nm, s.Vdb, basicAuthNm.Name)
	err := s.VRec.GetClient().Get(ctx, nm, curSvcMon)
	if err != nil && kerrors.IsNotFound(err) {
		s.Log.Info("creating service monitor", "Name", nm.Name)
		return s.VRec.GetClient().Create(ctx, expSvcMon)
	}

	if err != nil {
		return err
	}

	newSM := s.reconcileServiceMonitorFields(curSvcMon, expSvcMon)
	if newSM != nil {
		s.Log.Info("updating service monitor", "Name", nm.Name)
		return s.VRec.GetClient().Update(ctx, newSM)
	}
	return nil
}

// reconcileServiceMonitorFields updates the ServiceMonitor if any fields differ.
// Returns the updated ServiceMonitor or nil if no changes are needed.
func (s *ServiceMonitorReconciler) reconcileServiceMonitorFields(
	curSM, expSM *monitoringv1.ServiceMonitor,
) *monitoringv1.ServiceMonitor {
	updated := false

	// Reconcile annotations
	if stringMapDiffer(expSM.ObjectMeta.Annotations, curSM.ObjectMeta.Annotations) {
		curSM.ObjectMeta.Annotations = expSM.ObjectMeta.Annotations
		updated = true
	}

	// Reconcile labels
	if stringMapDiffer(expSM.ObjectMeta.Labels, curSM.ObjectMeta.Labels) {
		curSM.ObjectMeta.Labels = expSM.ObjectMeta.Labels
		updated = true
	}

	// Reconcile namespace selector
	if !reflect.DeepEqual(expSM.Spec.NamespaceSelector, curSM.Spec.NamespaceSelector) {
		curSM.Spec.NamespaceSelector = expSM.Spec.NamespaceSelector
		updated = true
	}

	// Reconcile service selector
	if !reflect.DeepEqual(expSM.Spec.Selector, curSM.Spec.Selector) {
		curSM.Spec.Selector = expSM.Spec.Selector
		updated = true
	}

	// Reconcile endpoints
	if !reflect.DeepEqual(expSM.Spec.Endpoints, curSM.Spec.Endpoints) {
		curSM.Spec.Endpoints = expSM.Spec.Endpoints
		updated = true
	}

	if updated {
		return curSM
	}

	return nil
}
