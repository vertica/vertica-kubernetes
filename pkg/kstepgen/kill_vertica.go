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
	"k8s.io/apimachinery/pkg/util/rand"
)

// CreateKillVerticaPodTestStep will generate a kuttl test step for killing a random number of pods
func CreateKillVerticaPodTestStep(log logr.Logger, wr io.Writer, locations *Locations, dbcfg *DatabaseCfg) (err error) {
	tin := makeKillVerticaPodInput(locations, dbcfg)
	log.Info("Creating kill vertica pod step", "vdb", dbcfg.VerticaDBName, "namespace", dbcfg.Namespace, "podsToKill", tin.PodsToKill)
	t, err := template.New("KillVerticaPods").Parse(KillVerticaPodTemplate)
	if err != nil {
		return err
	}

	err = t.Execute(wr, tin)
	if err != nil {
		return err
	}
	return nil
}

type killVerticaPodInput struct {
	ScriptDir  string
	PodsToKill int
	Namespace  string
}

var KillVerticaPodTemplate = `
apiVersion: kuttl.dev/v1beta1
kind: TestStep
commands:
  - command: {{ .ScriptDir }}/random-pod-deleter.sh -n {{ .Namespace }} {{ .PodsToKill }}
`

func makeKillVerticaPodInput(loc *Locations, dbcfg *DatabaseCfg) *killVerticaPodInput {
	return &killVerticaPodInput{
		ScriptDir:  loc.ScriptsDir,
		PodsToKill: rand.IntnRange(dbcfg.MinPodsToKill, dbcfg.MaxPodsToKill+1),
		Namespace:  dbcfg.Namespace,
	}
}
