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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ImageVersionReconciler will verify type of image deployment and set the version as annotations in the vdb.
type ImageVersionReconciler struct {
	VRec               *VerticaDBReconciler
	Log                logr.Logger
	Vdb                *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner            cmds.PodRunner
	PFacts             *PodFacts
	EnforceUpgradePath bool                    // Fail the reconcile if we find incompatible version
	FindPodFunc        func() (*PodFact, bool) // Function to call to find pod
}

// MakeImageVersionReconciler will build a VersionReconciler object
func MakeImageVersionReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts,
	enforceUpgradePath bool) controllers.ReconcileActor {
	return &ImageVersionReconciler{
		VRec:               vdbrecon,
		Log:                log.WithName("ImageVersionReconciler"),
		Vdb:                vdb,
		PRunner:            prunner,
		PFacts:             pfacts,
		EnforceUpgradePath: enforceUpgradePath,
		FindPodFunc:        pfacts.findRunningPod,
	}
}

// Reconcile will update the annotation in the Vdb with Vertica version info
func (v *ImageVersionReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	err := v.PFacts.Collect(ctx, v.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	pod, ok := v.FindPodFunc()
	if !ok {
		v.Log.Info("Could not find any running pod, requeuing reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	err = v.verifyImage(pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	var res ctrl.Result
	res, err = v.reconcileVersion(ctx, pod)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	var vinf *version.Info
	vinf, err = v.Vdb.MakeVersionInfoCheck()
	if err != nil {
		return ctrl.Result{}, err
	}

	if vinf.IsUnsupported(vapi.MinimumVersion) {
		v.VRec.Eventf(v.Vdb, corev1.EventTypeWarning, events.UnsupportedVerticaVersion,
			"The Vertica version %s is unsupported with this operator.  The minimum version supported is %s.",
			vinf.VdbVer, vapi.MinimumVersion)
		return ctrl.Result{Requeue: true}, nil
	}

	v.logWarningIfVersionDoesNotSupportsCGroupV2(ctx, vinf, pod)

	return ctrl.Result{}, nil
}

// logWarningIfVersionDoesNotSupportCGroupV2 will log a warning if it detects a
// 12.0.0 server and cgroups v2.  In such an environment you cannot start the
// server in k8s.
func (v *ImageVersionReconciler) logWarningIfVersionDoesNotSupportsCGroupV2(ctx context.Context, vinf *version.Info, pod *PodFact) {
	ver12, _ := version.MakeInfoFromStr(vapi.CGroupV2UnsupportedVersion)
	if !vinf.IsEqual(ver12) {
		return
	}

	// Check if the pod is running with cgroups v2
	cmd := []string{"test", "-f", "/sys/fs/cgroup/cgroup.controllers"}
	if _, _, err := v.PRunner.ExecInPod(ctx, pod.name, ServerContainer, cmd...); err == nil {
		// Log a warning but we will continue on.  We may have a hotfix that
		// addresses the bug so don't want to block any attempts to start vertica.
		v.VRec.Eventf(v.Vdb, corev1.EventTypeWarning, events.UnsupportedVerticaVersion,
			"The Vertica version is unsupported with cgroups v2. Try using a version other than %s.", vinf.VdbVer)
	}
}

// reconcileVersion will parse the version output and update any annotations.
func (v *ImageVersionReconciler) reconcileVersion(ctx context.Context, pod *PodFact) (ctrl.Result, error) {
	vver, err := v.getVersion(ctx, pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	return v.updateVDBVersion(ctx, vver)
}

// getVersion will get the Vertica version from the running pod.
func (v *ImageVersionReconciler) getVersion(ctx context.Context, pod *PodFact) (string, error) {
	stdout, _, err := v.PRunner.ExecInPod(ctx, pod.name, names.ServerContainer, "/opt/vertica/bin/vertica", "--version")
	if err != nil {
		return "", err
	}

	return stdout, nil
}

// updateVDBVersion will update the version that is stored in the vdb.  This may
// fail if it detects an invalid upgrade path.
func (v *ImageVersionReconciler) updateVDBVersion(ctx context.Context, newVersion string) (ctrl.Result, error) {
	versionAnnotations := vapi.ParseVersionOutput(newVersion)

	if v.EnforceUpgradePath && !v.Vdb.GetIgnoreUpgradePath() {
		ok, failureReason := v.Vdb.IsUpgradePathSupported(versionAnnotations)
		if !ok {
			v.VRec.Eventf(v.Vdb, corev1.EventTypeWarning, events.InvalidUpgradePath,
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
		if v.Vdb.MergeAnnotations(versionAnnotations) {
			err = v.VRec.Client.Update(ctx, v.Vdb)
			if err != nil {
				return err
			}
			v.Log.Info("Version annotation updated", "resourceVersion", v.Vdb.ResourceVersion,
				"version", v.Vdb.Annotations[vmeta.VersionAnnotation])
		}
		return nil
	})
}

// Verify whether the correct image is being used by checking the vclusterOps feature flag and the deployment type
func (v *ImageVersionReconciler) verifyImage(pod *PodFact) error {
	if vmeta.UseVClusterOps(v.Vdb.Annotations) {
		if pod.admintoolsExists {
			v.VRec.Eventf(v.Vdb, corev1.EventTypeWarning, events.WrongImage, "Image cannot be used for vclusterops deployments")
			return fmt.Errorf("image %s is meant for admintools style of deployments and cannot be used for vclusterops",
				v.Vdb.Spec.Image)
		}
	} else {
		if !pod.admintoolsExists {
			v.VRec.Eventf(v.Vdb, corev1.EventTypeWarning, events.WrongImage, "Image cannot be used for admintools deployments")
			return fmt.Errorf("image %s is meant for vclusterops style of deployments and cannot be used for admintools",
				v.Vdb.Spec.Image)
		}
	}
	return nil
}
