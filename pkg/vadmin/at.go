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

package vadmin

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
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
	if opcfg.GetDevMode() {
		a.PRunner.DumpAdmintoolsConf(ctx, initiatorPod)
	}
	stdout, _, err := a.PRunner.ExecAdmintools(ctx, initiatorPod, names.ServerContainer, cmd...)
	if opcfg.GetDevMode() {
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

// DestroyAuthParms will remove the auth parms file that was created in the pod
func (a *Admintools) destroyAuthParms(ctx context.Context, initiatorPod types.NamespacedName) {
	_, _, err := a.PRunner.ExecInPod(ctx, initiatorPod, names.ServerContainer,
		"rm", paths.AuthParmsFile,
	)
	if err != nil {
		// Destroying the auth parms is a best effort. If we fail to delete it,
		// the reconcile will continue on.
		a.Log.Info("failed to destroy auth parms, ignoring failure", "err", err)
	}
}

// genAuthParmsFileContent will generate the content to write into auth_parms.conf
func (a *Admintools) genAuthParmsFileContent(parms map[string]string) string {
	fileContent := strings.Builder{}
	for k, v := range parms {
		fileContent.WriteString(fmt.Sprintf("%s = %s\n", k, v))
	}
	return fileContent.String()
}

// initDB will perform the admintools call to initialize the db.
// It will be '-t create_db' cmd to create the db or '-t revive_db'
// cmd to revive the db.
func (a *Admintools) initDB(ctx context.Context, dbi DBInitializer) (ctrl.Result, error) {
	initiator := dbi.GetInitiator()
	if err := a.copyAuthFile(ctx, initiator, a.genAuthParmsFileContent(dbi.GetConfigParms())); err != nil {
		return ctrl.Result{}, err
	}
	defer a.destroyAuthParms(ctx, initiator)

	// Cleanup for any prior failed attempt.
	podNames := dbi.GetPodNames()
	for _, pod := range podNames {
		err := a.prepLocalData(ctx, pod)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	cmd := dbi.GenCmd()
	stdout, err := a.execAdmintools(ctx, initiator, cmd...)
	if err != nil {
		return dbi.LogFailure(stdout, err)
	}
	return ctrl.Result{}, nil
}

// prepLocalData Prepare for the add node or create_db by removing any local
// data/depot dirs and ensuring proper ownership.
// This step is necessary because of a lack of cleanup in admintools if any of
// these commands fail.
func (a *Admintools) prepLocalData(ctx context.Context, podName types.NamespacedName) error {
	locPaths := []string{a.VDB.GetDBDataPath(), a.VDB.GetDBDepotPath(), a.VDB.GetDBCatalogPath()}
	var rmCmds bytes.Buffer
	rmCmds.WriteString("set -o errexit\n")
	for _, path := range locPaths {
		rmCmds.WriteString(fmt.Sprintf("[[ -d %s ]] && rm -rf %s || true\n", path, path))
	}
	// We also need to ensure the dbadmin owns the depot directory.  When the
	// directory are first mounted they are owned by root.  Vertica handles changing
	// the ownership of the config, log and data directory.  This function exists to
	// handle the depot directory. This can be skipped if the depotPath is
	// shared with one of the data or catalog paths or if the depot volume is not
	// a PersistentVolume.
	if a.VDB.IsDepotVolumePersistentVolume() && a.VDB.Spec.Local.IsDepotPathUnique() {
		rmCmds.WriteString(fmt.Sprintf("sudo chown dbadmin:verticadba -R %s/%s", paths.LocalDataPath, a.VDB.GetPVSubPath("depot")))
	}
	cmd := []string{"bash", "-c", fmt.Sprintf("cat > %s<<< '%s'; bash %s",
		paths.PrepScript, rmCmds.String(), paths.PrepScript)}
	if _, _, err := a.PRunner.ExecInPod(ctx, podName, names.ServerContainer, cmd...); err != nil {
		return err
	}
	return nil
}
