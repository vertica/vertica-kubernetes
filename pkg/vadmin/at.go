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

package vadmin

import (
	"context"

	"github.com/vertica/vertica-kubernetes/pkg/mgmterrors"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Use this as a utility file that has functions common for multiple admintools
// commands.

// logFailure will log and record an event for an admintools failure
func (a Admintools) logFailure(cmd, genericFailureReason, op string, err error) (ctrl.Result, error) {
	evLogr := mgmterrors.MakeATErrors(a.EVWriter, a.VDB, genericFailureReason)
	return evLogr.LogFailure(cmd, op, err)
}

// execAdmintools is a wrapper for admintools tools that handles logging of
// debug information. The stdout and error of the AT call is returned.
func (a Admintools) execAdmintools(ctx context.Context, initiatorPod types.NamespacedName, cmd ...string) (string, error) {
	// Dump relevant contents of the admintools.conf before and after the
	// admintools calls. We do this for PD purposes to see what changes occurred
	// in the file.
	if a.DevMode {
		a.PRunner.DumpAdmintoolsConf(ctx, initiatorPod)
	}
	stdout, _, err := a.PRunner.ExecAdmintools(ctx, initiatorPod, names.ServerContainer, cmd...)
	if a.DevMode {
		a.PRunner.DumpAdmintoolsConf(ctx, initiatorPod)
	}
	return stdout, err
}
