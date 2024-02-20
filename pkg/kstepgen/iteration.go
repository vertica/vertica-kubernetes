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
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/rand"
)

type Iteration struct {
	log                 logr.Logger
	totalStepWeight     int
	totalDatabaseWeight int
	cfg                 *Config
	locations           *Locations
	finalAssert         *bytes.Buffer
}

func MakeIteration(log logr.Logger, locations *Locations, cfg *Config) Iteration {
	stepTot := cfg.StepTypeWeight.KillVerticaPod + cfg.StepTypeWeight.Scaling + cfg.StepTypeWeight.Sleep
	dbTot := 0
	for i := range cfg.Databases {
		dbTot += cfg.Databases[i].Weight
	}
	return Iteration{
		log:                 log,
		totalStepWeight:     stepTot,
		totalDatabaseWeight: dbTot,
		cfg:                 cfg,
		locations:           locations,
	}
}

func (it *Iteration) CreateIteration() error {
	it.log.Info("Creating new iteration", "stepCount", it.cfg.StepCount)
	if err := it.setupIteration(); err != nil {
		return err
	}
	for i := 0; i < it.cfg.StepCount; i++ {
		if err := it.createStep(i); err != nil {
			return err
		}
	}

	// Write out the final assert. This was generated from the final scaling event.
	if it.finalAssert != nil && it.finalAssert.Len() > 0 {
		if err := it.writeAssertBuffer(it.finalAssert, it.cfg.StepCount); err != nil {
			return fmt.Errorf("failed to write final assert: %w", err)
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
	stepBuffer, assertBuffer, writeAssertInThisStep, err := it.genTestStep()
	if err != nil {
		return err
	}

	fn := fmt.Sprintf("%s/%02d-step.yaml", it.locations.OutputDir, stepNum)
	stepFile, err := os.Create(fn)
	if err != nil {
		return fmt.Errorf("failed to open step file %s: %w", fn, err)
	}
	defer stepFile.Close()
	it.log.Info("Creating step", "name", stepFile.Name())
	_, err = stepFile.Write(stepBuffer.Bytes())
	if err != nil {
		return fmt.Errorf("failed to write step buffer %s: %w", fn, err)
	}

	// We always save the assert buffer for the final step. We can write the
	// same buffer out now in the current step.
	if assertBuffer.Len() > 0 {
		it.finalAssert = assertBuffer
	}
	if writeAssertInThisStep && assertBuffer.Len() > 0 {
		return it.writeAssertBuffer(assertBuffer, stepNum)
	}
	return nil
}

func (it *Iteration) writeAssertBuffer(assertBuffer *bytes.Buffer, stepNum int) error {
	fn := fmt.Sprintf("%s/%02d-assert.yaml", it.locations.OutputDir, stepNum)
	file, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer file.Close()
	it.log.Info("Creating assert", "file", file.Name())
	if _, err := file.Write(assertBuffer.Bytes()); err != nil {
		return fmt.Errorf("failed to write assert buffer %s: %w", fn, err)
	}
	return nil
}

func (it *Iteration) getRandomTestStep() int {
	r := rand.IntnRange(0, it.totalStepWeight)
	cum := it.cfg.StepTypeWeight.Scaling
	if r <= cum {
		return int(ScalingTestStep)
	}
	cum += it.cfg.StepTypeWeight.KillVerticaPod
	if r <= cum {
		return int(KillVerticaPodTestStep)
	}
	cum += it.cfg.StepTypeWeight.KillOperatorPod
	if r <= cum {
		return int(KillOperatorPodTestStep)
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

func (it *Iteration) genTestStep() (stepBuffer, assertBuffer *bytes.Buffer, writeAssertInThisStep bool, err error) {
	stepBuffer = new(bytes.Buffer)
	assertBuffer = new(bytes.Buffer)

	dbcfg := it.getRandomDatabase()
	switch it.getRandomTestStep() {
	case ScalingTestStep:
		if writeAssertInThisStep, err = CreateScalingTestStep(it.log, stepBuffer, assertBuffer, it.cfg, dbcfg); err != nil {
			return
		}

	case KillVerticaPodTestStep:
		if err = CreateKillVerticaPodTestStep(it.log, stepBuffer, it.locations, dbcfg); err != nil {
			return
		}

	case KillOperatorPodTestStep:
		if err = CreateKillOperatorPodTestStep(it.log, stepBuffer, it.cfg, it.locations); err != nil {
			return
		}

	case SleepTestStep:
		if err = CreateSleepTestStep(it.log, stepBuffer, dbcfg); err != nil {
			return
		}
	}
	return
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
