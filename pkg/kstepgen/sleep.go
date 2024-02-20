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

package kstepgen

import (
	"io"
	"text/template"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/rand"
)

// CreateSleepTestStep will generate a kuttl test step for sleeping a random amount of time
func CreateSleepTestStep(log logr.Logger, wr io.Writer, dbcfg *DatabaseCfg) (err error) {
	tin := makeSleepInput(dbcfg)
	log.Info("Creating sleep step", "sleepTime", tin.SleepTime)
	t, err := template.New("Sleep").Parse(SleepTemplate)
	if err != nil {
		return err
	}

	err = t.Execute(wr, tin)
	if err != nil {
		return err
	}
	return nil
}

type sleepInput struct {
	SleepTime int
	Timeout   int
}

var SleepTemplate = `
apiVersion: kuttl.dev/v1beta1
kind: TestStep
timeout: {{ .Timeout }}
commands:
  - command: sleep {{ .SleepTime }}
`

// makeSleepInput will create the sleepInput for the template based on the opts
func makeSleepInput(cfg *DatabaseCfg) *sleepInput {
	sleepTime := rand.IntnRange(cfg.MinSleepTime, cfg.MaxSleepTime+1)
	return &sleepInput{
		SleepTime: sleepTime,
		Timeout:   sleepTime + 30, // Must be >= SleepTime, 30 added as a buffer
	}
}
