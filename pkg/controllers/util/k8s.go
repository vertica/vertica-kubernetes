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

package util

import (
	"context"

	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func FetchVDB(ctx context.Context, vrec config.ReconcilerInterface, obj runtime.Object,
	nm types.NamespacedName, vdb *v1.VerticaDB) (ctrl.Result, error) {
	err := vrec.GetClient().Get(ctx, nm, vdb)
	if err != nil && errors.IsNotFound(err) {
		vrec.Eventf(obj, corev1.EventTypeWarning, events.VerticaDBNotFound,
			"The VerticaDB named '%s' was not found", nm.Name)
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, err
}
