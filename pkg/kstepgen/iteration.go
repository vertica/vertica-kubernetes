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
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/util/rand"
)

const (
	ScalingTestStep = iota
	KillPodTestStep
	SleepTestStep
	LastTestStep  = SleepTestStep
	FirstTestStep = ScalingTestStep
)

var DefaultTestStepWeight = []int{1, 2, 5}

type Iteration struct {
	totalWeight int
	opts        *Options
	assertBuf   bytes.Buffer
	file        *os.File
}

func MakeIteration(opts *Options) Iteration {
	tot := 0
	for _, w := range opts.StepTypeWeight {
		tot += w
	}
	return Iteration{
		totalWeight: tot,
		opts:        opts,
	}
}

func (it *Iteration) CreateIteration() error {
	if err := it.setupIteration(); err != nil {
		return err
	}
	for i := 0; i < it.opts.StepCount; i++ {
		fn := fmt.Sprintf("%s/%02d-step.yaml", it.opts.OutputDir, i)
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
	}
	if it.assertBuf.Len() > 0 {
		file, err := os.Create(fmt.Sprintf("%s/%02d-assert.yaml", it.opts.OutputDir, it.opts.StepCount))
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
	file, err := os.Create(fmt.Sprintf("%s/%02d-wait-for-steady-state.yaml", it.opts.OutputDir, it.opts.StepCount+1))
	if err != nil {
		return err
	}
	defer file.Close()
	return CreateSteadyStateStep(file, it.opts)
}

func (it *Iteration) getRandomTestStep() int {
	r := rand.IntnRange(0, it.totalWeight)
	cum := 0
	for i, w := range it.opts.StepTypeWeight {
		cum += w
		if r <= cum {
			return i
		}
	}
	return LastTestStep
}

func (it *Iteration) genTestStep() error {
	switch it.getRandomTestStep() {
	case ScalingTestStep:
		if err := CreateScalingTestStep(it.file, &it.assertBuf, it.opts); err != nil {
			return err
		}

	case KillPodTestStep:
		if err := CreateKillPodTestStep(it.file, it.opts); err != nil {
			return err
		}

	case SleepTestStep:
		if err := CreateSleepTestStep(it.file, it.opts); err != nil {
			return err
		}
	}
	return nil
}

func (it *Iteration) setupIteration() error {
	const DefaultPermissions = 0755
	if err := os.MkdirAll(it.opts.OutputDir, DefaultPermissions); err != nil {
		return err
	}
	files, err := filepath.Glob(fmt.Sprintf("%s/??-*.yaml", it.opts.OutputDir))
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
