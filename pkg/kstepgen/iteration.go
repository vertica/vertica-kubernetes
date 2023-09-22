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
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/util/rand"
)

type Iteration struct {
	totalStepWeight     int
	totalDatabaseWeight int
	cfg                 *Config
	assertBuf           bytes.Buffer
	file                *os.File
	locations           *Locations
}

func MakeIteration(locations *Locations, cfg *Config) Iteration {
	stepTot := cfg.StepTypeWeight.KillPod + cfg.StepTypeWeight.Scaling + cfg.StepTypeWeight.Sleep
	dbTot := 0
	for i := range cfg.Databases {
		dbTot += cfg.Databases[i].Weight
	}
	return Iteration{
		totalStepWeight:     stepTot,
		totalDatabaseWeight: dbTot,
		cfg:                 cfg,
		locations:           locations,
	}
}

func (it *Iteration) CreateIteration() error {
	if err := it.setupIteration(); err != nil {
		return err
	}
	for i := 0; i < it.cfg.StepCount; i++ {
		if err := it.createStep(i); err != nil {
			return err
		}
	}
	if it.assertBuf.Len() > 0 {
		file, err := os.Create(fmt.Sprintf("%s/%02d-assert.yaml", it.locations.OutputDir, it.cfg.StepCount))
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := file.Write(it.assertBuf.Bytes()); err != nil {
			return err
		}
	}

	// Write out the final steady state step.  This waits for the operator to
	// finish a reconciliation cycle without any work needed.
	file, err := os.Create(fmt.Sprintf("%s/%02d-wait-for-steady-state.yaml", it.locations.OutputDir, it.cfg.StepCount+1))
	if err != nil {
		return err
	}
	defer file.Close()
	return CreateSteadyStateStep(file, it.locations, it.cfg)
}

func (it *Iteration) createStep(stepNum int) error {
	fn := fmt.Sprintf("%s/%02d-step.yaml", it.locations.OutputDir, stepNum)
	var err error
	it.file, err = os.Create(fn)
	if err != nil {
		return err
	}
	defer it.file.Close()

	err = it.genTestStep()
	if err != nil {
		return err
	}
	return nil
}

func (it *Iteration) getRandomTestStep() int {
	r := rand.IntnRange(0, it.totalStepWeight)
	cum := it.cfg.StepTypeWeight.Scaling
	if r <= cum {
		return int(ScalingTestStep)
	}
	cum += it.cfg.StepTypeWeight.KillPod
	if r <= cum {
		return int(KillPodTestStep)
	}
	return int(SleepTestStep)
}

func (it *Iteration) getRandomDatabase() *DatabaseCfg {
	// If no weights are calculated each database is equally likely.
	if it.totalDatabaseWeight == 0 {
		r := rand.IntnRange(0, len(it.cfg.Databases))
		return &it.cfg.Databases[r]
	}

	r := rand.IntnRange(0, it.totalDatabaseWeight)
	cum := 0
	for i := range it.cfg.Databases {
		cum += it.cfg.Databases[i].Weight
		if r <= cum {
			return &it.cfg.Databases[i]
		}
	}
	return &it.cfg.Databases[len(it.cfg.Databases)-1]
}

func (it *Iteration) genTestStep() error {
	dbcfg := it.getRandomDatabase()
	switch it.getRandomTestStep() {
	case ScalingTestStep:
		if err := CreateScalingTestStep(it.file, &it.assertBuf, it.cfg, dbcfg); err != nil {
			return err
		}

	case KillPodTestStep:
		if err := CreateKillPodTestStep(it.file, it.locations, dbcfg); err != nil {
			return err
		}

	case SleepTestStep:
		if err := CreateSleepTestStep(it.file, dbcfg); err != nil {
			return err
		}
	}
	return nil
}

func (it *Iteration) setupIteration() error {
	const DefaultPermissions = 0755
	if err := os.MkdirAll(it.locations.OutputDir, DefaultPermissions); err != nil {
		return err
	}
	files, err := filepath.Glob(fmt.Sprintf("%s/??-*.yaml", it.locations.OutputDir))
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}
