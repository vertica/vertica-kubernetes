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
	"fmt"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"k8s.io/apimachinery/pkg/types"
)

// cleanupLocalFiles Prepare for the add node or create_db by removing any local
// data/depot dirs.
// This step is necessary because of a lack of cleanup in admintools if any of
// these commands fail.
func cleanupLocalFiles(ctx context.Context, vdb *vapi.VerticaDB, prunner cmds.PodRunner, podName types.NamespacedName) error {
	locPaths := []string{vdb.GetDBDataPath(), vdb.GetDepotPath()}
	for _, path := range locPaths {
		cmd := []string{"rm", "-r", path}

		if _, stderr, err := prunner.ExecInPod(ctx, podName, names.ServerContainer, cmd...); err != nil {
			// We ignore not found errors since the path is already gone
			if !strings.Contains(stderr, "No such file or directory") {
				return err
			}
		}
	}
	return nil
}

// debugDumpAdmintoolsConf will dump specific info from admintools.conf for logging purposes
// +nolint
func debugDumpAdmintoolsConf(ctx context.Context, prunner cmds.PodRunner, atPod types.NamespacedName) {
	// Dump out vital informating from admintools.conf for logging purposes. We
	// rely on the logging that is done inside ExecInPod.
	cmd := []string{
		"bash", "-c",
		fmt.Sprintf(`ls -l %s && grep '^node\|^v_\|^host' %s`, paths.AdminToolsConf, paths.AdminToolsConf),
	}
	// Since this is for debugging purposes all errors are ignored
	prunner.ExecInPod(ctx, atPod, names.ServerContainer, cmd...) // nolint:errcheck
}

// debugDumpAdmintoolsConfForPods will dump debug information for admintools.conf for a list of pods
func debugDumpAdmintoolsConfForPods(ctx context.Context, prunner cmds.PodRunner, pods []*PodFact) {
	for _, pod := range pods {
		debugDumpAdmintoolsConf(ctx, prunner, pod.name)
	}
}

// changeDepotPermissions ensures dbadmin owns the depot directory.  When the
// directory are first mounted they are owned by root.  Vertica handles changing
// the ownership of the config, log and data directory.  This function exists to
// handle the depot directory.
func changeDepotPermissions(ctx context.Context, vdb *vapi.VerticaDB, prunner cmds.PodRunner, podList []*PodFact) error {
	cmd := []string{
		"sudo", "chown", "dbadmin:verticadba", "-R", fmt.Sprintf("%s/%s", paths.LocalDataPath, vdb.GetPVSubPath("depot")),
	}
	for _, pod := range podList {
		if _, _, err := prunner.ExecInPod(ctx, pod.name, names.ServerContainer, cmd...); err != nil {
			return err
		}
	}
	return nil
}
