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
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/installpackages"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// InstallPackagesReconciler will install all packages under /opt/vertica/packages where Autoinstall is marked true
type InstallPackagesReconciler struct {
	Log        logr.Logger
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner    cmds.PodRunner
	PFacts     *PodFacts
	Dispatcher vadmin.Dispatcher
}

// MakeInstallPackagesReconciler will build a InstallPackagesReconciler object
func MakeInstallPackagesReconciler(
	vdbrecon *VerticaDBReconciler, vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts,
	dispatcher vadmin.Dispatcher,
	log logr.Logger,
) controllers.ReconcileActor {
	return &InstallPackagesReconciler{
		Log:        log.WithName("InstallPackagesReconciler"),
		VRec:       vdbrecon,
		Vdb:        vdb,
		PRunner:    prunner,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

// Reconcile will force install default packages in the running database
func (i *InstallPackagesReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if i.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyCreateSkipPackageInstall {
		return ctrl.Result{}, nil
	}
	err := i.PFacts.Collect(ctx, i.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	// No-op if no database exists
	if !i.PFacts.doesDBExist() {
		return ctrl.Result{}, nil
	}

	// Force reinstall default packages
	if i.PFacts.getUpNodeCount() > 0 {
		return i.installPackagesInPod(ctx)
	}
	// Retry if no up nodes
	i.Log.Info("Could not find any running pod, requeuing reconciliation.")
	return ctrl.Result{Requeue: true}, nil
}

// installPackagesInPod will find one pod to initiate the process of installing default packages
func (i *InstallPackagesReconciler) installPackagesInPod(ctx context.Context) (ctrl.Result, error) {
	pf, ok := i.PFacts.findPodToRunAdminCmdAny()
	if !ok {
		// If no suitable pod found, there is nowhere to install packages
		// and we should retry
		i.Log.Info("Could not find any target to issue install packages command, requeuing reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	// Run the install_packages command
	return ctrl.Result{}, i.runCmd(ctx, pf.name, pf.podIP)
}

// runCmd issues the admintools or vclusterops command to force install the default packages
func (i *InstallPackagesReconciler) runCmd(ctx context.Context, initiatorName types.NamespacedName, initiatorIP string) error {
	i.VRec.Event(i.Vdb, corev1.EventTypeNormal, events.InstallPackagesStarted, "Starting install packages")
	start := time.Now()
	opts := []installpackages.Option{
		installpackages.WithInitiator(initiatorName, initiatorIP),
		installpackages.WithForceReinstall(true),
	}
	if err := i.Dispatcher.InstallPackages(ctx, opts...); err != nil {
		return err
	}
	i.VRec.Eventf(i.Vdb, corev1.EventTypeNormal, events.InstallPackagesSucceeded,
		"Successfully installed packages. It took %s", time.Since(start).Truncate(time.Second))
	return nil
}
