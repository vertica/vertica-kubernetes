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
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VerifyDeploymentReconciler will verify the current deployment supports
// sandboxing with vclusterops
type VerifyDeploymentReconciler struct {
	SRec *SandboxConfigMapReconciler
	Vdb  *v1.VerticaDB
	Log  logr.Logger
}

func MakeVerifyDeploymentReconciler(r *SandboxConfigMapReconciler,
	vdb *v1.VerticaDB, log logr.Logger) controllers.ReconcileActor {
	return &VerifyDeploymentReconciler{
		SRec: r,
		Log:  log.WithName("VerifyDeploymentReconciler"),
		Vdb:  vdb,
	}
}

// Reconcile will verify the current deployment supports
// sandboxing with vclusterops
func (s *VerifyDeploymentReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	return s.checkDeployment()
}

// checkDeployment checks if version supports sandboxing with vclusterops
func (s *VerifyDeploymentReconciler) checkDeployment() (ctrl.Result, error) {
	if !vmeta.UseVClusterOps(s.Vdb.Annotations) {
		s.SRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.VclusterOpsDisabled,
			"The VerticaDB named '%s' has vclusterops disabled", s.Vdb.Name)
		return ctrl.Result{Requeue: true}, nil
	}
	vinf, err := s.Vdb.MakeVersionInfoCheck()
	if err != nil {
		return ctrl.Result{}, err
	}
	if !vinf.IsEqualOrNewer(v1.SandboxSupportedMinVersion) {
		s.SRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.SandboxNotSupported,
			"The Vertica version %q does not support sandboxing with vclusterops",
			vinf.VdbVer)
		return ctrl.Result{Requeue: true}, nil
	}
	s.Log.Info(fmt.Sprintf("The VerticaDB named '%s' is configured for sandboxing with vclusterops", s.Vdb.Name))
	return ctrl.Result{}, nil
}
