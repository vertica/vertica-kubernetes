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
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/getconfigparameter"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetConfigurationParameterReconciler will get configuration parameters from the database
type GetConfigurationParameterReconciler struct {
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	Vdb         *vapi.VerticaDB // Vdb is the CRD we are acting on
	PFacts      *PodFacts
	InitiatorIP string // The IP of the pod that we run vclusterOps from
	Dispatcher  vadmin.Dispatcher
	client.Client
	ConfigParameter string
	Level           string
	RetrievedValue  *string
}

// MakeGetConfigurationParameterReconciler will build a GetConfigurationParameterReconciler object
func MakeGetConfigurationParameterReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher, cli client.Client, configParameter,
	level string, retrievedValue *string /*out param*/) controllers.ReconcileActor {
	return &GetConfigurationParameterReconciler{
		VRec:            vdbrecon,
		Log:             log.WithName("getConfigurationParameterReconciler"),
		Vdb:             vdb,
		PFacts:          pfacts,
		Dispatcher:      dispatcher,
		Client:          cli,
		ConfigParameter: configParameter,
		Level:           level,
		RetrievedValue:  retrievedValue,
	}
}

func (s *GetConfigurationParameterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	var res ctrl.Result

	// We need to collect pod facts for finding qualified subclusters
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return res, nil
	}

	res, value, err := s.getConfigurationParameter(ctx)
	if err != nil {
		return res, err
	}

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.GetConfigurationParameterSucceeded,
		"Successfully got configuration parameter %q with value %q at %q level", s.ConfigParameter, value, s.Level)

	*s.RetrievedValue = value

	return res, nil
}

// getConfigurationParameter calls the vclusterOps API to get a configuration parameter
func (s *GetConfigurationParameterReconciler) getConfigurationParameter(ctx context.Context) (ctrl.Result, string, error) {
	// select the first up (and not read-only) pod in the given cluster as the initiator
	initiator, ok := s.PFacts.findFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	if !ok {
		s.Log.Info("No Up nodes found. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, "", nil
	}

	levelForEvent := s.Level
	if levelForEvent == "" {
		// leave level empty implies database level
		levelForEvent = "database"
	}

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.GetConfigurationParameterStarted,
		"Starting to get configuration parameter %q at %q level", s.ConfigParameter, levelForEvent)

	value, err := s.Dispatcher.GetConfigurationParameter(ctx,
		getconfigparameter.WithUserName(s.Vdb.GetVerticaUser()),
		getconfigparameter.WithInitiatorIP(initiator.podIP),
		getconfigparameter.WithSandbox(s.PFacts.GetSandboxName()), // sandbox name is empty string for main cluster
		getconfigparameter.WithConfigParameter(s.ConfigParameter),
		getconfigparameter.WithLevel(s.Level),
	)
	if err != nil {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.GetConfigurationParameterFailed,
			"Failed to get configuration parameter %q at %q level", s.ConfigParameter, levelForEvent)
		return ctrl.Result{}, "", err
	}
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.GetConfigurationParameterSucceeded,
		"Successfully got configuration parameter %q with value %q at %q level", s.ConfigParameter, value, levelForEvent)

	return ctrl.Result{}, value, nil
}
