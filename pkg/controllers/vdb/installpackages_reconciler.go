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
) controllers.ReconcileActor {
	return &InstallPackagesReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		PRunner:    prunner,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

// Reconcile will force install default packages in the running database
func (s *InstallPackagesReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if s.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyCreateSkipPackageInstall {
		return ctrl.Result{}, nil
	}
	err := s.PFacts.Collect(ctx, s.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	// No-op if no database exists
	if !s.PFacts.doesDBExist() {
		return ctrl.Result{Requeue: true}, nil
	}

	// Force reinstall default packages
	if s.PFacts.getUpNodeCount() > 0 {
		err = s.installPackagesInPod(ctx)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, err
}

// installPackagesInPod will find one pod to initiate the process of installing default packages
func (s *InstallPackagesReconciler) installPackagesInPod(ctx context.Context) error {
	pf, ok := s.PFacts.findPodToRunAdminCmdAny()
	if !ok {
		// If no running pod found, then there is nowhere to install packages
		// and we can just continue on
		return nil
	}

	// Run the install_packages command
	err := s.runCmd(ctx, pf.name, pf.podIP)

	return err
}

// runCmd issues the admintools or vclusterops command to force install the default packages
func (s *InstallPackagesReconciler) runCmd(ctx context.Context, initiatorName types.NamespacedName, initiatorIP string) error {
	s.VRec.Event(s.Vdb, corev1.EventTypeNormal, events.InstallPackagesStarted, "Starting install packages")
	start := time.Now()
	opts := []installpackages.Option{
		installpackages.WithInitiator(initiatorName, initiatorIP),
		installpackages.WithForceReinstall(true),
	}
	if err := s.Dispatcher.InstallPackages(ctx, opts...); err != nil {
		s.VRec.Event(s.Vdb, corev1.EventTypeWarning, events.InstallPackagesFailed, "Failed to install packages")
		return err
	}
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.InstallPackagesSucceeded,
		"Successfully installed packages. It took %s", time.Since(start).Truncate(time.Second))
	return nil
}
