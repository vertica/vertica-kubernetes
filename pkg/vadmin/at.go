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
	"strings"

	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Use this as a utility file that has functions common for multiple admintools
// commands.

// logFailure will log and record an event for an admintools failure
func (a *Admintools) logFailure(cmd, genericFailureReason, op string, err error) (ctrl.Result, error) {
	evLogr := aterrors.MakeATErrors(a.EVWriter, a.VDB, genericFailureReason)
	return evLogr.LogFailure(cmd, op, err)
}

// execAdmintools is a wrapper for admintools tools that handles logging of
// debug information. The stdout and error of the AT call is returned.
func (a *Admintools) execAdmintools(ctx context.Context, initiatorPod types.NamespacedName, cmd ...string) (string, error) {
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

// copyAuthFile will copy the auth file into the container
func (a *Admintools) copyAuthFile(ctx context.Context, initiatorPod types.NamespacedName, content string) error {
	_, _, err := a.PRunner.ExecInPod(ctx, initiatorPod, names.ServerContainer,
		"bash", "-c", fmt.Sprintf("cat > %s<<< '%s'", paths.AuthParmsFile, content))

	// We log an event for this error because it could be caused by bad values
	// in the creds.  If the value we get out of the secret has undisplayable
	// characters then we won't even be able to copy the file.
	if err != nil {
		a.EVWriter.Eventf(a.VDB, corev1.EventTypeWarning, events.AuthParmsCopyFailed,
			"Failed to copy auth parms to the pod '%s'", initiatorPod)
	}
	return err
}

// genAuthParmsFileContent will generate the content to write into auth_parms.conf
func genAuthParmsFileContent(parms map[string]string) string {
	fileContent := strings.Builder{}
	for k, v := range parms {
		fileContent.WriteString(fmt.Sprintf("%s = %s\n", k, v))
	}
	return fileContent.String()
}
