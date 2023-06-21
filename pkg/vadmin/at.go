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
	"fmt"

	"github.com/vertica/vertica-kubernetes/pkg/mgmterrors"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
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

// debugDumpAdmintoolsConf will dump specific info from admintools.conf for logging purposes
// +nolint
func (a Admintools) debugDumpAdmintoolsConf(ctx context.Context, atPod types.NamespacedName) {
	// Dump out vital informating from admintools.conf for logging purposes. We
	// rely on the logging that is done inside ExecInPod.
	cmd := []string{
		"bash", "-c",
		fmt.Sprintf(`ls -l %s && grep '^node\|^v_\|^host' %s`, paths.AdminToolsConf, paths.AdminToolsConf),
	}
	// Since this is for debugging purposes all errors are ignored
	a.PRunner.ExecInPod(ctx, atPod, names.ServerContainer, cmd...) //nolint:errcheck
}
