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
	"fmt"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// UpgradeOperatorReconciler will handle any upgrade actions for k8s
// objects created in older operators.
type UpgradeOperatorReconciler struct {
	VRec *VerticaDBReconciler
	Log  logr.Logger
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
}

// MakeUpgradeOperatorReconciler will build a UpgradeOperatorFromReconciler object
func MakeUpgradeOperatorReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &UpgradeOperatorReconciler{VRec: vdbrecon, Log: log.WithName("UpgradeOperatorReconciler"), Vdb: vdb}
}

// Reconcile will handle any upgrade actions for k8s objects created in older operators.
func (u *UpgradeOperatorReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	finder := iter.MakeSubclusterFinder(u.VRec.Client, u.Vdb)
	stss, err := finder.FindStatefulSets(ctx, iter.FindExisting|iter.FindSkipSandboxFilter, vapi.MainCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := u.deleteOldSts(ctx, stss.Items); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, u.SetTLSEnabled(ctx, stss.Items)
}

// deleteOldSts will delete any statefulsets created by an older version (prior to 2.2.0) of the
// operator.
func (u *UpgradeOperatorReconciler) deleteOldSts(ctx context.Context, stss []appsv1.StatefulSet) error {
	// In 2.2.0, we changed the selector labels for statefulsets.  But selector
	// labels are immutable.  So to upgrade to this current version we need to
	// delete any sts created in prior releases.
	for i := range stss {
		sts := &stss[i]
		opVer, ok := sts.ObjectMeta.Labels[vmeta.OperatorVersionLabel]
		if !ok {
			u.Log.Info("skipping object since we could not find operator version label", "name", sts.Name)
			continue
		}
		if opVer < vmeta.OperatorVersion220 {
			u.VRec.Event(u.Vdb, corev1.EventTypeNormal, events.OperatorUpgrade,
				fmt.Sprintf("Recreating statefulset '%s' because it was created by an older version of the operator (%s)",
					sts.Name, opVer))
			if err := u.VRec.Client.Delete(ctx, sts); err != nil {
				u.Log.Info("Error deleting old statefulset", "opVer", opVer)
				return err
			}
		}
	}

	return nil
}

// SetTLSEnabled will set the TLS Enabled fields in the vdb spec if they
// are nil and the operator version is older than 25.4.0.  This was added
// in 25.4.0 to make it easier to pick the right tls settings during an upgrade.
func (u *UpgradeOperatorReconciler) SetTLSEnabled(ctx context.Context, stss []appsv1.StatefulSet) error {
	if !u.Vdb.ShouldSetTLSEnabled() {
		return nil
	}
	for i := range stss {
		sts := &stss[i]
		opVer, ok := sts.ObjectMeta.Labels[vmeta.OperatorVersionLabel]
		u.Log.Info("checking operator version for statefulset", "name", sts.Name, "opVer", opVer)
		if !ok {
			u.Log.Info("skipping object since we could not find operator version label", "name", sts.Name)
			continue
		}
		vInf, ok := version.MakeInfoFromStr(fmt.Sprintf("v%s", opVer))
		if !ok {
			u.Log.Info("skipping object since we could not parse operator version", "name", sts.Name, "opVer", opVer)
			continue
		}
		if vInf.IsOlder(fmt.Sprintf("v%s", vmeta.OperatorVersion254)) {
			u.Log.Info("updating vdb TLS settings based on tls auth annotation")
			// If the operator version is older than 25.4.0, then we need to
			// set the TLS settings in the vdb based on the annotation.
			// This was added in 25.4.0 to make it easier to pick the right
			// tls settings during an upgrade.
			return u.updateTLSInVdb(ctx)
		}
	}

	return nil
}

// updateTLSInVdb will set the TLS Enabled fields in the vdb spec based
// on the tls auth annotation.
func (u *UpgradeOperatorReconciler) updateTLSInVdb(ctx context.Context) error {
	enabled := vmeta.UseTLSAuth(u.Vdb.Annotations)
	nm := u.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest in case we are in the retry loop
		if err := u.VRec.Client.Get(ctx, nm, u.Vdb); err != nil {
			return err
		}

		if u.Vdb.Spec.HTTPSNMATLS != nil && u.Vdb.Spec.HTTPSNMATLS.Enabled == nil {
			u.Vdb.Spec.HTTPSNMATLS.Enabled = new(bool)
			*u.Vdb.Spec.HTTPSNMATLS.Enabled = enabled
		}
		if u.Vdb.Spec.ClientServerTLS != nil && u.Vdb.Spec.ClientServerTLS.Enabled == nil {
			u.Vdb.Spec.ClientServerTLS.Enabled = new(bool)
			*u.Vdb.Spec.ClientServerTLS.Enabled = enabled
		}

		return u.VRec.Client.Update(ctx, u.Vdb)
	})
}
