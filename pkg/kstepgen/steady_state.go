/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

// CreateSleepTestStep will generate a kuttl test step for sleeping a random amount of time
func CreateSteadyStateStep(wr io.Writer, opts *Options) (err error) {
	tin := makeSteadyStateInput(opts)
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
	Options
	StepTimeout int
}

var SteadyStateTemplate = `
apiVersion: kuttl.dev/v1beta1
kind: TestStep
timeout: {{ .StepTimeout }}
commands:
  - command: {{ .ScriptDir }}/wait-for-verticadb-steady-state.sh -n {{ .Namespace }} -t {{ .SteadyStateTimeout }}
`

// makeSteadyStateInput will create the steadyStateInput for the template
func makeSteadyStateInput(opts *Options) *steadyStateInput {
	// 5 added as a time buffer to to account for wait-for-verticadb-steady-state.sh startup
	const TimeBuffer = 5
	return &steadyStateInput{
		Options:     *opts,
		StepTimeout: opts.SteadyStateTimeout + TimeBuffer,
	}
}
