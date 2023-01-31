/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// LocalDataCheckReconciler will check the free space available in the PV and
// log events if they it is too low.
type LocalDataCheckReconciler struct {
	VRec      *VerticaDBReconciler
	Vdb       *vapi.VerticaDB
	PFacts    *PodFacts
	NumEvents int // Number of events written
}

// MakeLocalDataCheckReconciler will build a LocalDataCheckReconciler object
func MakeLocalDataCheckReconciler(vdbrecon *VerticaDBReconciler, vdb *vapi.VerticaDB, pfacts *PodFacts) controllers.ReconcileActor {
	return &LocalDataCheckReconciler{VRec: vdbrecon, Vdb: vdb, PFacts: pfacts}
}

// Reconcile will look at all pods to see if any have low disk space available.
func (l *LocalDataCheckReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := l.PFacts.Collect(ctx, l.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// We report a warning for any pod that has less then this amount of free
	// space in their local PV.
	const FreeSpaceThreshold = 10 * 1024 * 1024 // 10mb
	pods := l.PFacts.findPodsLowOnDiskSpace(FreeSpaceThreshold)
	l.NumEvents = 0
	for i := range pods {
		l.VRec.Eventf(l.Vdb, corev1.EventTypeWarning, events.LowLocalDataAvailSpace,
			"Low disk space in persistent volume attached to %s", pods[i].name.Name)
		l.NumEvents++
	}

	return ctrl.Result{}, nil
}
