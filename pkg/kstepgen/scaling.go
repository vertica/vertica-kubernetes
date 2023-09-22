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
	"fmt"
	"io"
	"text/template"

	"k8s.io/apimachinery/pkg/util/rand"
)

// CreateScalingTestStep will generate kuttl test step for random scaling
func CreateScalingTestStep(stepWriter, assertWriter io.Writer, cfg *Config, dbcfg *DatabaseCfg) (err error) {
	tin := makeTemplateInput(cfg, dbcfg)
	if err := generateVerticaDB(stepWriter, tin); err != nil {
		return err
	}
	return generateKuttlAssert(assertWriter, tin)
}

type scalingInput struct {
	Config
	PodCount    int
	Subclusters []subclusterDetail
}

type subclusterDetail struct {
	Name      string
	Size      int
	IsPrimary bool
}

var VerticaCRDTemplate = `
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: {{ .Name }}
spec:
  subclusters:
    {{- range .Subclusters }}
    - name: {{ .Name }}
      size: {{ .Size }}
      isPrimary: {{ .IsPrimary }}
	{{- end }}
`

var KuttlAssertTemplate = `
{{- $vdbName := .Name }}
{{- range .Subclusters }}
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: {{ $vdbName }}-{{ .Name }}
status:
  replicas: {{ .Size }}
  readyReplicas: {{ .Size }}
---
{{- end }}
apiVersion: vertica.com/v1beta1
kind: VerticaDB
metadata:
  name: {{ .Name }}
status:
  installCount: {{ .PodCount }}
  upNodeCount: {{ .PodCount }}
  addedToDBCount: {{ .PodCount }}
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
		Config:      *cfg,
		PodCount:    rand.IntnRange(dbcfg.MinPods, dbcfg.MaxPods+1),
		Subclusters: []subclusterDetail{},
	}
	numSubclusters := rand.Intn(dbcfg.MaxSubclusters-dbcfg.MinSubclusters+1) + dbcfg.MinSubclusters
	podsAssigned := 0
	for i := 0; i < numSubclusters; i++ {
		scMaxPod := tin.PodCount - podsAssigned - (numSubclusters - i - 1)
		scMinPod := 1
		if i+1 == numSubclusters {
			scMinPod = scMaxPod
		}
		sc := subclusterDetail{
			Name:      fmt.Sprintf("sc%d", i),
			Size:      rand.Intn(scMaxPod-scMinPod+1) + scMinPod,
			IsPrimary: getRandomIsPrimary(),
		}
		// First subcluster is always primary
		if i == 0 {
			sc.IsPrimary = true
		}
		tin.Subclusters = append(tin.Subclusters, sc)
		podsAssigned += sc.Size
	}
	return tin
}

// getRandomIsPrimary randomly picks primary or secondary subcluster
func getRandomIsPrimary() bool {
	const (
		IsPrimaryRandRangeMin = 0
		IsPrimaryRandRangeMax = 100
		IsPrimaryRandPercent  = 75
	)
	return rand.IntnRange(IsPrimaryRandRangeMin, IsPrimaryRandRangeMax) < IsPrimaryRandPercent
}
