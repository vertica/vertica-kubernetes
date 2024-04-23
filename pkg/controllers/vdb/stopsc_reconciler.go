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
	"github.com/pkg/errors"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopsc"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type StopSubclusterReconciler struct {
	Rec            config.ReconcilerInterface
	Vdb            *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner        cmds.PodRunner
	Log            logr.Logger
	PFacts         *PodFacts
	SCName         string
	InitiatorPodIP string
	Dispatcher     vadmin.Dispatcher
}

func MakeStopSubclusterReconciler(rec config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, scName string, pfacts *PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &StopSubclusterReconciler{
		Rec:        rec,
		Log:        log,
		Vdb:        vdb,
		SCName:     scName,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

func (s *StopSubclusterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if s.SCName == "" {
		return ctrl.Result{}, errors.New("no subcluster provided")
	}
	err := s.PFacts.Collect(ctx, s.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	ip, ok := s.getFirstUpSCPodIP()
	if !ok {
		s.Log.Info("No stop subcluster needed. All nodes are down", "scName", s.SCName)
		return ctrl.Result{}, nil
	}
	// set the initiator
	s.InitiatorPodIP = ip
	err = s.stopSubcluster(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Invalidate the cached pod facts now that a subcluster is shutdown
	s.PFacts.Invalidate()

	s.Rec.Eventf(s.Vdb, corev1.EventTypeNormal, events.StopSubclusterSucceeded,
		"Successfully stopped subcluster %q", s.SCName)
	return ctrl.Result{}, nil
}

// getFirstUpSCPodIP finds and return the first up node in the given
// subcluster
func (s *StopSubclusterReconciler) getFirstUpSCPodIP() (string, bool) {
	return s.PFacts.FindFirstUpPodIP(true, s.SCName)
}

// stopSubcluster calls the API that will perform stop subcluster
func (s *StopSubclusterReconciler) stopSubcluster(ctx context.Context) error {
	opts := []stopsc.Option{
		stopsc.WithInitiator(s.InitiatorPodIP),
		stopsc.WithSubcluster(s.SCName),
	}
	return s.Dispatcher.StopSubcluster(ctx, opts...)
}
