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
	"regexp"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VersionReconciler will set the version as annotations in the vdb.
type VersionReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

// MakeVersionReconciler will build a VersinReconciler object
func MakeVersionReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &VersionReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts}
}

func (v *VersionReconciler) GetClient() client.Client {
	return v.VRec.Client
}

func (v *VersionReconciler) GetVDB() *vapi.VerticaDB {
	return v.Vdb
}

func (v *VersionReconciler) CollectPFacts(ctx context.Context) error {
	return v.PFacts.Collect(ctx, v.Vdb)
}

// Reconcile will update the annotation in the Vdb with Vertica version info
func (v *VersionReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := v.PFacts.Collect(ctx, v.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	pod, ok := v.PFacts.findRunningPod()
	if !ok {
		v.Log.Info("Could not find any running pod, requeuing reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	if err := v.reconcileVersion(ctx, pod); err != nil {
		return ctrl.Result{}, err
	}

	vinf, ok := version.MakeInfo(v.Vdb)
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
func (v *VersionReconciler) reconcileVersion(ctx context.Context, pod *PodFact) error {
	versionAnnotations, err := v.buildVersionAnnotations(ctx, pod)
	if err != nil {
		return err
	}

	if v.mergeAnnotations(versionAnnotations) {
		err = v.VRec.Client.Update(ctx, v.Vdb)
		if err != nil {
			return err
		}
	}
	return nil
}

// buildVersionAnnotations will build a map of annotations based on the --version output
func (v *VersionReconciler) buildVersionAnnotations(ctx context.Context, pod *PodFact) (map[string]string, error) {
	stdout, _, err := v.PRunner.ExecInPod(ctx, pod.name, ServerContainer, "/opt/vertica/bin/vertica", "--version")
	if err != nil {
		return nil, err
	}

	return v.parseVersionOutput(stdout), nil
}

// mergeAnnotations will merge new annotations with vdb.  It will return true if
// any annotation changed.  Caller is responsible for updating the Vdb in the
// API server.
func (v *VersionReconciler) mergeAnnotations(newAnnotations map[string]string) bool {
	changedAnnotations := false
	for k, newValue := range newAnnotations {
		oldValue, ok := v.Vdb.ObjectMeta.Annotations[k]
		if !ok || oldValue != newValue {
			if v.Vdb.ObjectMeta.Annotations == nil {
				v.Vdb.ObjectMeta.Annotations = map[string]string{}
			}
			v.Vdb.ObjectMeta.Annotations[k] = newValue
			changedAnnotations = true
		}
	}
	return changedAnnotations
}

// parseVersionOutput will parse the raw output from the --version call and
// build an annotation map.
// nolint:lll
func (v *VersionReconciler) parseVersionOutput(op string) map[string]string {
	// Sample output looks like this:
	// Vertica Analytic Database v11.0.0-20210601
	// vertica(v11.0.0-20210601) built by @re-docker2 from master@da8f0e93f1ee720d8e4f8e1366a26c0d9dd7f9e7 on 'Tue Jun  1 05:04:35 2021' $BuildId$
	regMap := map[string]string{
		vapi.VersionAnnotation:   `(v[0-9a-zA-Z.-]+)\n`,
		vapi.BuildRefAnnotation:  `built by .* from .*@([^ ]+) `,
		vapi.BuildDateAnnotation: `on '([A-Za-z0-9: ]+)'`,
	}

	// We build up this annotation map while we iterate through each regular
	// expression
	annotations := map[string]string{}

	for annName, reStr := range regMap {
		r := regexp.MustCompile(reStr)
		parse := r.FindStringSubmatch(op)
		const MinStringMatch = 2 // [0] is for the entire string, [1] is for the submatch
		if len(parse) >= MinStringMatch {
			annotations[annName] = parse[1]
		}
	}
	return annotations
}
