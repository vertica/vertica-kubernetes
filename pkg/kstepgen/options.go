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

// Options contains parameters to influence the behavior of the kuttl step generation
type Options struct {
	OutputDir      string
	StepCount      int
	StepTypeWeight []int

	ScriptDir     string
	MinPodsToKill int
	MaxPodsToKill int

	MinSleepTime int
	MaxSleepTime int

	MinSubclusters int
	MaxSubclusters int
	MinPods        int
	MaxPods        int

	Name      string
	Namespace string

	SteadyStateTimeout int
}

func MakeDefaultOptions() *Options {
	return &Options{
		OutputDir:      "/root/git/vertica-kubernetes/gen-test",
		StepCount:      3,
		StepTypeWeight: DefaultTestStepWeight,
		Name:           "v",
		Namespace:      "soak",
	}
}
