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
	"io/ioutil"
	"os"

	"github.com/lithammer/dedent"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
)

// acceptEulaIfMissing will accept the end user license agreement if any pods have not yet signed it
func acceptEulaIfMissing(ctx context.Context, pFacts *PodFacts, pRunner cmds.PodRunner) error {
	for _, p := range pFacts.Detail {
		if p.eulaAccepted || !p.isPodRunning {
			continue
		}
		if err := acceptEulaInPod(ctx, p, pRunner); err != nil {
			return err
		}
	}
	return nil
}

// acceptEulaInPod will run a script that will accept the eula in the given pod
func acceptEulaInPod(ctx context.Context, pf *PodFact, pRunner cmds.PodRunner) error {
	tmp, err := ioutil.TempFile("", "accept_eula.py.")
	if err != nil {
		return err
	}
	defer tmp.Close()
	defer os.Remove(tmp.Name())

	// A python script that will accept the eula
	acceptEulaPython := `
		import vertica.shared.logging
		import vertica.tools.eula_checker
		vertica.shared.logging.setup_admintool_logging()
		vertica.tools.eula_checker.EulaChecker().write_acceptance()
	`
	_, err = tmp.WriteString(dedent.Dedent(acceptEulaPython))
	if err != nil {
		return err
	}
	tmp.Close()

	_, _, err = pRunner.CopyToPod(ctx, pf.name, names.ServerContainer, tmp.Name(), paths.EulaAcceptanceScript)
	if err != nil {
		return err
	}

	_, _, err = pRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "/opt/vertica/oss/python3/bin/python3", paths.EulaAcceptanceScript)
	if err != nil {
		return err
	}
	return nil
}
