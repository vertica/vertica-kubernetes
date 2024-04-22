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

package vrep

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	ctrl "sigs.k8s.io/controller-runtime"
)

func fetchSourceAndTargetVDBs(ctx context.Context,
	vRec *VerticaReplicatorReconciler,
	vrep *v1beta1.VerticaReplicator) (vdbSource, vdbTarget *vapi.VerticaDB, res ctrl.Result, err error) {
	vdbSource = &vapi.VerticaDB{}
	vdbTarget = &vapi.VerticaDB{}
	nmSource := names.GenNamespacedName(vrep, vrep.Spec.Source.VerticaDB)
	nmTarget := names.GenNamespacedName(vrep, vrep.Spec.Target.VerticaDB)
	if res, err = vk8s.FetchVDB(ctx, vRec, vrep, nmSource, vdbSource); verrors.IsReconcileAborted(res, err) {
		return nil, nil, res, err
	}
	if res, err = vk8s.FetchVDB(ctx, vRec, vrep, nmTarget, vdbTarget); verrors.IsReconcileAborted(res, err) {
		return nil, nil, res, err
	}
	return vdbSource, vdbTarget, ctrl.Result{}, nil
}
