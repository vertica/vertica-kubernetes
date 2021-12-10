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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	ctrl "sigs.k8s.io/controller-runtime"
)

// OnlineImageChangeReconciler will handle the process when the vertica image
// changes.  It does this while keeping the database online.
type OnlineImageChangeReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
	Finder  SubclusterFinder
	Manager ImageChangeManager
}

// MakeOnlineImageChangeReconciler will build an OnlineImageChangeReconciler object
func MakeOnlineImageChangeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &OfflineImageChangeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts,
		Finder:  MakeSubclusterFinder(vdbrecon.Client, vdb),
		Manager: *MakeImageChangeManager(vdbrecon, log, vdb, onlineImageChangeAllowed),
	}
}

// Reconcile will handle the process of the vertica image changing.  For
// example, this can automate the process for an upgrade.
func (o *OnlineImageChangeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if ok, err := o.Manager.IsImageChangeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	if err := o.PFacts.Collect(ctx, o.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an image change by setting condition and event recording
		o.Manager.startImageChange,
		// Cleanup up the condition and event recording for a completed image change
		o.Manager.finishImageChange,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx); res.Requeue || err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}
