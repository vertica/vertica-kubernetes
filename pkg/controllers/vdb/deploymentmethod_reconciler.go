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

package vdb

import (
	"context"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"

	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DeploymentMethodReconciler will handle deployment method changes
type DeploymentMethodReconciler struct {
	VRec       config.ReconcilerInterface
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts     *podfacts.PodFacts
	PRunner    cmds.PodRunner
	Manager    UpgradeManager
	Dispatcher vadmin.Dispatcher
}

// MakeDeploymentMethodReconciler will build a DeploymentMethodReconciler object
func MakeDeploymentMethodReconciler(vdbrecon config.ReconcilerInterface, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &DeploymentMethodReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("DeploymentMethodReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Manager:    *MakeUpgradeManager(vdbrecon, log, vdb, "", nil),
		Dispatcher: dispatcher,
		PRunner:    prunner,
	}
}

func (d *DeploymentMethodReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-ip when database is not initialized
	if !d.Vdb.IsDBInitialized() {
		return ctrl.Result{}, nil
	}

	// For admintools deployment, we only need to update deploymentMethod in status
	if !vmeta.UseVClusterOps(d.Vdb.Annotations) {
		if d.Vdb.Status.DeploymentMethod != vapi.DeploymentMethodAT {
			err := d.updateDeploymentMethodInStatus(ctx, vapi.DeploymentMethodAT)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// when deployment is switching to vclusterOps, enable HTTPS TLS if needed
	if d.Vdb.Status.DeploymentMethod != vapi.DeploymentMethodVC {
		res, err := d.reconcileHTTPSTLS(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		return ctrl.Result{}, d.updateDeploymentMethodInStatus(ctx, vapi.DeploymentMethodVC)
	}
	return ctrl.Result{}, nil
}

// reconcileHTTPSTLS will enable HTTPS TLS if it's not enabled
func (d *DeploymentMethodReconciler) reconcileHTTPSTLS(ctx context.Context) (ctrl.Result, error) {
	pf, ok := d.PFacts.FindFirstUpPod(false, "")
	if !ok {
		d.Log.Info("No up pod found to check https tls, restarting the database")
		actor := MakeRestartReconciler(d.VRec, d.Log, d.Vdb, d.PRunner, d.PFacts, false, d.Dispatcher)
		d.Manager.traceActorReconcile(actor)
		res, err := actor.Reconcile(ctx, &ctrl.Request{})
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		// after restart, we need to recollect pfacts
		err = d.PFacts.Collect(ctx, d.Vdb)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return d.Manager.enableHTTPSTLSIfNeeded(ctx, d.PFacts, pf)
}

func (d *DeploymentMethodReconciler) updateDeploymentMethodInStatus(ctx context.Context, deploymentMethod string) error {
	// update local Vdb as well, it will make sure next vdb read can get the new value
	d.Vdb.Status.DeploymentMethod = deploymentMethod
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		vdbChg.Status.DeploymentMethod = deploymentMethod
		return nil
	}

	return vdbstatus.Update(ctx, d.VRec.GetClient(), d.Vdb, updateStatus)
}
