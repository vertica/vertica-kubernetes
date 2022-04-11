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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VersionReconciler will set the version as annotations in the vdb.
type VersionReconciler struct {
	VRec               *VerticaDBReconciler
	Log                logr.Logger
	Vdb                *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner            cmds.PodRunner
	PFacts             *PodFacts
	EnforceUpgradePath bool                    // Fail the reconcile if we find incompatible version
	FindPodFunc        func() (*PodFact, bool) // Function to call to find pod
}

// MakeVersionReconciler will build a VersionReconciler object
func MakeVersionReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts,
	enforceUpgradePath bool) controllers.ReconcileActor {
	return &VersionReconciler{
		VRec:               vdbrecon,
		Log:                log,
		Vdb:                vdb,
		PRunner:            prunner,
		PFacts:             pfacts,
		EnforceUpgradePath: enforceUpgradePath,
		FindPodFunc:        pfacts.findRunningPod,
	}
}

// Reconcile will update the annotation in the Vdb with Vertica version info
func (v *VersionReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := v.PFacts.Collect(ctx, v.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	pod, ok := v.FindPodFunc()
	if !ok {
		v.Log.Info("Could not find any running pod, requeuing reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	if res, err := v.reconcileVersion(ctx, pod); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	vinf, ok := version.MakeInfoFromVdb(v.Vdb)
	if !ok {
		// Version info is not in the vdb.  Fine to skip.
		return ctrl.Result{}, nil
	}

	if vinf.IsUnsupported() {
		v.VRec.EVRec.Eventf(v.Vdb, corev1.EventTypeWarning, events.UnsupportedVerticaVersion,
			"The Vertica version %s is unsupported with this operator.  The minimum version supported is %s.",
			vinf.VdbVer, version.MinimumVersion)
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

// reconcileVersion will parse the version output and update any annotations.
func (v *VersionReconciler) reconcileVersion(ctx context.Context, pod *PodFact) (ctrl.Result, error) {
	vver, err := v.getVersion(ctx, pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	return v.updateVDBVersion(ctx, vver)
}

// getVersion will get the Vertica version from the running pod.
func (v *VersionReconciler) getVersion(ctx context.Context, pod *PodFact) (string, error) {
	stdout, _, err := v.PRunner.ExecInPod(ctx, pod.name, names.ServerContainer, "/opt/vertica/bin/vertica", "--version")
	if err != nil {
		return "", err
	}

	return stdout, nil
}

// updateVDBVersion will update the version that is stored in the vdb.  This may
// fail if it detects an invalid upgrade path.
func (v *VersionReconciler) updateVDBVersion(ctx context.Context, newVersion string) (ctrl.Result, error) {
	versionAnnotations := version.ParseVersionOutput(newVersion)

	if v.EnforceUpgradePath && !v.Vdb.Spec.IgnoreUpgradePath {
		ok, failureReason := version.IsUpgradePathSupported(v.Vdb, versionAnnotations)
		if !ok {
			v.VRec.EVRec.Eventf(v.Vdb, corev1.EventTypeWarning, events.InvalidUpgradePath,
				"Invalid upgrade path detected.  %s", failureReason)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch to get latest Vdb incase this is a retry
		err := v.VRec.Client.Get(ctx, v.Vdb.ExtractNamespacedName(), v.Vdb)
		if err != nil {
			return err
		}
		if version.MergeAnnotations(v.Vdb, versionAnnotations) {
			err = v.VRec.Client.Update(ctx, v.Vdb)
			if err != nil {
				return err
			}
		}
		return nil
	})
}
