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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NMARunningModeReconciler will verify the running mode of NMA and report error if the running mode is not supported yet
type NMARunningModeReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

// MakeNMARunningModeReconciler will build a NMARunningModeReconciler object
func MakeNMARunningModeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &NMARunningModeReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("NMARunningModeReconciler"),
	}
}

// Reconcile will verify the running mode of NMA
func (n *NMARunningModeReconciler) Reconcile(_ context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	err := n.verifyNMARunningMode()
	return ctrl.Result{}, err
}

// Verify whether the NMA is configured to run in sidecar container and report error if so
func (n *NMARunningModeReconciler) verifyNMARunningMode() error {
	if n.Vdb.IsSideCarDeploymentEnabled() {
		vinf, err := n.Vdb.MakeVersionInfoCheck()
		if err != nil {
			return err
		}
		if vinf.IsEqualOrNewer(vapi.NMAInSideCarDeploymentMinVersion) {
			return nil
		}
		errMsg := fmt.Sprintf("running NMA in a sidecar container is not supported for version %s",
			n.Vdb.Annotations[vmeta.VersionAnnotation])
		n.VRec.Eventf(n.Vdb, corev1.EventTypeWarning, events.NMAInSidecarNotSupported,
			errMsg)
		return errors.New(errMsg)
	}
	return nil
}
