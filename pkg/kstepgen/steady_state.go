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
)

// CreateSteadyStateStep will generate a kuttl test step that will wait for the
// operator to get to the steady state and no more reconcile iterations.
func CreateSteadyStateStep(wr io.Writer, locations *Locations, cfg *Config) (err error) {
	tin := makeSteadyStateInput(locations, cfg)
	t, err := template.New("SteadyState").Parse(SteadyStateTemplate)
	if err != nil {
		return err
	}

	err = t.Execute(wr, tin)
	if err != nil {
		return err
	}
	return nil
}

type steadyStateInput struct {
	Config
	ScriptDir   string
	StepTimeout int
}

var SteadyStateTemplate = `
apiVersion: kuttl.dev/v1beta1
kind: TestStep
timeout: {{ .StepTimeout }}
commands:
  - command: {{ .ScriptDir }}/wait-for-verticadb-steady-state.sh -n {{ .OperatorNamespace }} -t {{ .SteadyStateTimeout }}
`

// makeSteadyStateInput will create the steadyStateInput for the template
func makeSteadyStateInput(locations *Locations, cfg *Config) *steadyStateInput {
	// 5 added as a time buffer to to account for wait-for-verticadb-steady-state.sh startup
	const TimeBuffer = 5
	return &steadyStateInput{
		Config:      *cfg,
		StepTimeout: cfg.SteadyStateTimeout + TimeBuffer,
		ScriptDir:   locations.ScriptsDir,
	}
}
