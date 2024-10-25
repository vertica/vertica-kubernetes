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
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopsubcluster"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SubclusterShutdownReconciler will handle the process when subclusters
// needs to be shut down or restart
type SubclusterShutdownReconciler struct {
	VRec       *VerticaDBReconciler
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
	Manager    UpgradeManager
}

// MakeSubclusterShutdownReconciler will build a SubclusterShutdownReconciler object
func MakeSubclusterShutdownReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &SubclusterShutdownReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("SubclusterShutdownReconciler"),
		Vdb:        vdb,
		Dispatcher: dispatcher,
		PFacts:     pfacts,
	}
}

func (s *SubclusterShutdownReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op as there is no subclusters
	if len(s.Vdb.Spec.Subclusters) == 0 {
		return ctrl.Result{}, nil
	}
	for i := range s.Vdb.Spec.Subclusters {
		sc := &s.Vdb.Spec.Subclusters[i]
		res, err := s.reconcileSubclusterShutdown(ctx, sc)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileSubclusterShutdown updates the subclusters configmap in order to
// trigger shutdown or restart in a subcluster.
func (s *SubclusterShutdownReconciler) reconcileSubclusterShutdown(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	scName := sc.Name
	scMap := s.Vdb.GenSubclusterMap()
	scStatusMap := s.Vdb.GenSubclusterStatusMap()

	if scMap == nil || scStatusMap == nil {
		s.Log.Info("Requeue because the subcluster or its state does not exist yet", "subcluster name: ", scName)
		return ctrl.Result{Requeue: true}, nil
	}

	hostIP, ok := s.PFacts.FindFirstUpPodIP(false, scName)
	if !ok {
		s.Log.Info("No running pod for stop subcluster. Requeuing.")
		return ctrl.Result{Requeue: true}, nil
	}

	// TODO: Call shutdown subcluster API and update status of trigger
	err := s.runStopSubclusterVclusterAPI(ctx, hostIP, scName)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// runStopSubclusterVclusterAPI will do the actual execution of stop subcluster.
// This handles logging of necessary events.
func (s *SubclusterShutdownReconciler) runStopSubclusterVclusterAPI(ctx context.Context,
	host string, scName string) error {
	opts := s.genStopSubclusterOpts(host, scName)
	s.VRec.Event(s.Vdb, corev1.EventTypeNormal, events.StopSubclusterStart, "Starting task stop subcluster")
	start := time.Now()

	err := s.Dispatcher.VStopSubcluster(ctx, opts...)
	if err != nil {
		// For all other errors, return error
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.StopSubclusterFailed,
			"Failed to stop subcluster %q", scName)
		return err
	}

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.StopSubclusterSucceeded,
		"Successfully stop subcluster. It took %s", time.Since(start).Truncate(time.Second))
	return nil
}

// genStopSubclusterOpts will return the options to use with the stop subcluster api
func (s *SubclusterShutdownReconciler) genStopSubclusterOpts(initiatorIP string, scName string) []stopsubcluster.Option {
	opts := []stopsubcluster.Option{
		stopsubcluster.WithInitiator(initiatorIP),
		stopsubcluster.WithSCName(scName),
	}
	return opts
}
