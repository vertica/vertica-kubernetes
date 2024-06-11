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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/setconfigparameter"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SetConfigurationParameterReconciler will set configuration parameters in the database
type SetConfigurationParameterReconciler struct {
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	Vdb         *vapi.VerticaDB // Vdb is the CRD we are acting on
	PFacts      *PodFacts
	InitiatorIP string // The IP of the pod that we run vclusterOps from
	Dispatcher  vadmin.Dispatcher
	client.Client
	ConfigParameter string
	Value           string
	Level           string
}

// MakeSetConfigurationParameterReconciler will build a SetConfigurationParameterReconciler object
func MakeSetConfigurationParameterReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher, cli client.Client, configParameter, value, level string) controllers.ReconcileActor {
	return &SetConfigurationParameterReconciler{
		VRec:            vdbrecon,
		Log:             log.WithName("setConfigurationParameterReconciler"),
		Vdb:             vdb,
		PFacts:          pfacts,
		Dispatcher:      dispatcher,
		Client:          cli,
		ConfigParameter: configParameter,
		Value:           value,
		Level:           level,
	}
}

func (s *SetConfigurationParameterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	var res ctrl.Result

	// We need to collect pod facts for finding qualified subclusters
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return res, nil
	}

	res, err := s.setConfigurationParameter(ctx)
	if err != nil {
		return res, err
	}

	return res, nil
}

// setConfigurationParameter calls the vclusterOps API to set a configuration parameter
func (s *SetConfigurationParameterReconciler) setConfigurationParameter(ctx context.Context) (ctrl.Result, error) {
	initiator, ok := s.PFacts.findFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	if !ok {
		s.Log.Info("No Up nodes found. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	levelForEvent := s.Level
	if levelForEvent == "" {
		levelForEvent = "database"
	}

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.SetConfigurationParameterStarted,
		"Starting to set configuration parameter %q to value %q at %q level", s.ConfigParameter, s.Value, levelForEvent)

	err := s.Dispatcher.SetConfigurationParameter(ctx,
		setconfigparameter.WithUserName(s.Vdb.GetVerticaUser()),
		setconfigparameter.WithInitiatorIP(initiator.podIP),
		setconfigparameter.WithSandbox(s.PFacts.GetSandboxName()),
		setconfigparameter.WithConfigParameter(s.ConfigParameter),
		setconfigparameter.WithValue(s.Value),
		setconfigparameter.WithLevel(s.Level),
	)
	if err != nil {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.SetConfigurationParameterFailed,
			"Failed to set configuration parameter %q to value %q at %q level", s.ConfigParameter, s.Value, levelForEvent)
		return ctrl.Result{}, err
	}
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.SetConfigurationParameterSucceeded,
		"Successfully set configuration parameter %q to value %q at %q level", s.ConfigParameter, s.Value, levelForEvent)

	return ctrl.Result{}, nil
}
