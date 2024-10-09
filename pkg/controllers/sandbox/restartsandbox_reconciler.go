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

package sandbox

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vdbcontroller "github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RestartSandboxReconciler will make sure the sandbox and its subclusters'
// shutdown states are properly set after sandbox restart.
type RestartSandboxReconciler struct {
	SRec      *SandboxConfigMapReconciler
	Vdb       *v1.VerticaDB
	Log       logr.Logger
	PFacts    *vdbcontroller.PodFacts
	RestartSC bool
}

func MakeRestartSandboxReconciler(r *SandboxConfigMapReconciler,
	vdb *v1.VerticaDB, pfacts *vdbcontroller.PodFacts, log logr.Logger) controllers.ReconcileActor {
	return &RestartSandboxReconciler{
		SRec:   r,
		Log:    log.WithName("RestartSandboxReconciler"),
		Vdb:    vdb,
		PFacts: pfacts,
	}
}

func (r *RestartSandboxReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if res, err := r.updateSandboxShutdownState(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateSubclustersShutdownState(ctx)
}

// updateSandboxShutdownState updates the subcluster's shutdown state after
// it's been restarted.
func (r *RestartSandboxReconciler) updateSandboxShutdownState(ctx context.Context) (ctrl.Result, error) {
	sb := r.Vdb.GetSandbox(r.PFacts.GetSandboxName())
	// If spec shutdown is true, there's nothing to do
	// as the operator is still not allowed to restart
	// the sandbox
	if sb == nil || sb.Shutdown {
		return ctrl.Result{}, nil
	}
	sbStatus := r.Vdb.GetSandboxStatus(r.PFacts.GetSandboxName())
	// If status shutdown is equal to spec shutdown, there is nothing
	// to do
	if sbStatus == nil || !sbStatus.Shutdown {
		r.RestartSC = true
		return ctrl.Result{}, nil
	}
	if err := r.PFacts.Collect(ctx, r.Vdb); err != nil {
		return ctrl.Result{}, err
	}
	// If there is no up node we requeue to first restart the sandbox
	if r.PFacts.GetUpNodeAndNotReadOnlyCount() == 0 {
		r.Log.Info("Restart cluster is needed. Requeueing")
		return ctrl.Result{Requeue: true}, nil
	}
	err := vdbstatus.SetSandboxShutdownState(ctx, r.SRec.GetClient(), r.Vdb, r.PFacts.GetSandboxName(), false)
	r.RestartSC = true
	return ctrl.Result{}, err
}

// updateSubclustersShutdownState updates the sandbox's subclusters shutdown state after
// the sandbox has been restarted.
func (r *RestartSandboxReconciler) updateSubclustersShutdownState(ctx context.Context) error {
	if !r.RestartSC {
		return nil
	}
	_, err := vk8s.UpdateVDBWithRetry(ctx, r.SRec, r.Vdb, r.updateSubclustersShutdownStateCallback)
	return err
}

func (r *RestartSandboxReconciler) updateSubclustersShutdownStateCallback() (bool, error) {
	sb := r.Vdb.GetSandbox(r.PFacts.SandboxName)
	if sb == nil {
		return false, errors.New("sandbox not found")
	}
	needUpdate := false
	scMap := r.Vdb.GenSubclusterMap()
	for i := range sb.Subclusters {
		sc := scMap[sb.Subclusters[i].Name]
		if sc.Shutdown {
			sc.Shutdown = false
			needUpdate = true
		}
	}
	return needUpdate, nil
}
