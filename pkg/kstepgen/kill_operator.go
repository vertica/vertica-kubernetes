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

package kstepgen

import (
	"io"
	"text/template"

	"github.com/go-logr/logr"
)

// CreateKillOperatorPodTestStep will generate a kuttl test step to kill the operator
func CreateKillOperatorPodTestStep(log logr.Logger, wr io.Writer, cfg *Config, loc *Locations) (err error) {
	tin := makeKillOperatorPodInput(cfg, loc)
	log.Info("Creating kill operator pod step", "namespace", cfg.OperatorNamespace)
	t, err := template.New("KillOperatorPods").Parse(killOperatorPodTemplate)
	if err != nil {
		return err
	}

	err = t.Execute(wr, tin)
	if err != nil {
		return err
	}
	return nil
}

type killOperatorPodInput struct {
	Namespace  string
	ScriptsDir string
}

var killOperatorPodTemplate = `
apiVersion: kuttl.dev/v1beta1
kind: TestStep
commands:
  - command: kubectl -n {{ .Namespace }} delete pod -l control-plane=controller-manager
  - command: {{ .ScriptsDir }}/wait-for-webhook.sh -n {{ .Namespace }}
`

func makeKillOperatorPodInput(cfg *Config, loc *Locations) *killOperatorPodInput {
	return &killOperatorPodInput{
		Namespace:  cfg.OperatorNamespace,
		ScriptsDir: loc.ScriptsDir,
	}
}
