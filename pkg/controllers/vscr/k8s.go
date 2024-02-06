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

package vscr

import (
	"context"

	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/util"
	ctrl "sigs.k8s.io/controller-runtime"
)

// fetchVDB will fetch the VerticaDB that is referenced in a VerticaScrutinize.
// This will log an event if the VerticaDB is not found.
func fetchVDB(ctx context.Context, vrec *VerticaScrutinizeReconciler,
	vscr *vapi.VerticaScrutinize, vdb *v1.VerticaDB) (ctrl.Result, error) {
	return util.FetchVDB(ctx, vrec, vscr, vscr.ExtractVDBNamespacedName(), vdb)
}
