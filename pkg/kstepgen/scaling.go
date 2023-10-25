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

// CreateScalingTestStep will generate kuttl test step for random scaling
func CreateScalingTestStep(log logr.Logger, stepWriter, assertWriter io.Writer, cfg *Config, dbcfg *DatabaseCfg) (bool, error) {
	tin := makeTemplateInput(cfg, dbcfg)
	log.Info("Creating scaling step", "vdb", dbcfg.VerticaDBName, "namespace", dbcfg.Namespace, "totalPodCount", tin.TotalPodCount)
	if err := generateVerticaDB(stepWriter, tin); err != nil {
		return false, err
	}
	if err := generateKuttlAssert(assertWriter, tin); err != nil {
		return false, err
	}
	log.Info("Skip generating scaling assert")
	return shouldGenerateScalingAssert(dbcfg), nil
}

type scalingInput struct {
	Config
	DBCfg         *DatabaseCfg
	Subclusters   []subclusterDetail
	TotalPodCount int
}

type subclusterDetail struct {
	Name string
	Size int
	Type string
}

var VerticaCRDTemplate = `
apiVersion: vertica.com/v1
kind: VerticaDB
metadata:
  name: {{ .DBCfg.VerticaDBName }}
  namespace: {{ .DBCfg.Namespace }}
spec:
  subclusters:
    {{- range .Subclusters }}
    - name: {{ .Name }}
      size: {{ .Size }}
      type: {{ .Type }}
	{{- end }}
`

var KuttlAssertTemplate = `
{{- $vdbName := .DBCfg.VerticaDBName }}
{{- $vdbNamespace := .DBCfg.Namespace }}
{{- range .Subclusters }}
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ $vdbName }}-{{ .Name }}
  namespace: {{ $vdbNamespace }}
status:
  replicas: {{ .Size }}
  readyReplicas: {{ .Size }}
---
{{- end }}
apiVersion: vertica.com/v1
kind: VerticaDB
metadata:
  name: {{ $vdbName }}
  namespace: {{ $vdbNamespace }}
status:
  upNodeCount: {{ .TotalPodCount }}
  addedToDBCount: {{ .TotalPodCount }}
  subclusterCount: {{ len .Subclusters }}
`

func generateVerticaDB(wr io.Writer, tin *scalingInput) error {
	t, err := template.New("VerticaDB").Parse(VerticaCRDTemplate)
	if err != nil {
		return err
	}

	err = t.Execute(wr, tin)
	if err != nil {
		return err
	}
	return nil
}

func generateKuttlAssert(wr io.Writer, tin *scalingInput) error {
	t, err := template.New("Kuttl Assert").Parse(KuttlAssertTemplate)
	if err != nil {
		return err
	}

	err = t.Execute(wr, tin)
	if err != nil {
		return err
	}
	return nil
}

// makeTemplateInput will fill out a templateInput and return it
func makeTemplateInput(cfg *Config, dbcfg *DatabaseCfg) *scalingInput {
	tin := &scalingInput{
		Config:        *cfg,
		DBCfg:         dbcfg,
		Subclusters:   []subclusterDetail{},
		TotalPodCount: 0,
	}
	for _, sc := range dbcfg.Subclusters {
		newSize := rand.IntnRange(sc.MinSize, sc.MaxSize+1)
		if newSize == 0 && sc.RemoveWhenZero {
			continue
		}
		tin.TotalPodCount += newSize
		tin.Subclusters = append(tin.Subclusters, subclusterDetail{
			Name: sc.Name,
			Type: sc.Type,
			Size: newSize,
		})
	}
	return tin
}

func shouldGenerateScalingAssert(dbcfg *DatabaseCfg) bool {
	const (
		PctMin = 0
		PctMax = 100
	)
	assertChance := rand.IntnRange(PctMin, PctMax+1)
	return assertChance <= dbcfg.PctAssertScaling
}
