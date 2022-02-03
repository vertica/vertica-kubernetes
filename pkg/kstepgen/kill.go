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

package kstepgen

import (
	"io"
	"text/template"

	"k8s.io/apimachinery/pkg/util/rand"
)

// CreateKillPodTestStep will generate a kuttl test step for killing a random number of pods
func CreateKillPodTestStep(wr io.Writer, opts *Options) (err error) {
	tin := makeKillPodInput(opts)
	t, err := template.New("KillPods").Parse(KillPodTemplate)
	if err != nil {
		return err
	}

	err = t.Execute(wr, tin)
	if err != nil {
		return err
	}
	return nil
}

type killPodInput struct {
	ScriptDir  string
	PodsToKill int
}

var KillPodTemplate = `
apiVersion: kuttl.dev/v1beta1
kind: TestStep
commands:
  - command: {{ .ScriptDir }}/random-pod-deleter.sh -n $NAMESPACE {{ .PodsToKill }}
    namespaced: true
`

func makeKillPodInput(opts *Options) *killPodInput {
	return &killPodInput{
		ScriptDir:  opts.ScriptDir,
		PodsToKill: rand.IntnRange(opts.MinPodsToKill, opts.MaxPodsToKill+1),
	}
}
