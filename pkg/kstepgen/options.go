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

// Config contains parameters to influence the behavior of the kuttl step generation
type Config struct {
	// The name of the namespace where the operator is deployed
	OperatorNamespace string `json:"operatorNamespace"`
	// The number of steps to produce
	StepCount int `json:"stepCount"`
	// The weights of each type of steps. Higher values make that type of step
	// more likely to occur.
	StepTypeWeight StepTypeWeights `json:"stepTypeWeight"`
	// The amount of time to wait for the operator to get to a steady state,
	// meaning it isn't actively running a reconcile iteration. The steady state
	// is an indication that operator successfully reconciled everything
	// according to the VerticaDB changes.
	SteadyStateTimeout int `json:"steadyStateTimeout"`

	// Config for each database that you want to test against.
	Databases []DatabaseCfg `json:"databases"`
}

type StepTypeWeights struct {
	Scaling int `json:"scaling"`
	KillPod int `json:"killPod"`
	Sleep   int `json:"sleep"`
}

const (
	ScalingTestStep = iota
	KillPodTestStep
	SleepTestStep
	// When adding new steps be sure to include it in StepTypeWeights
	LastTestStep  = SleepTestStep
	FirstTestStep = ScalingTestStep
)

type DatabaseCfg struct {
	// The name of the VerticaDB CR for this database
	VerticaDBName string `json:"verticaDBName"`
	// The namespace the VerticaDB CR is found in
	Namespace string `json:"namespace"`

	// The weight of this database. The higher the weight, the more likely it
	// will be chosen for a test step.
	Weight int `json:"weight,omitempty"`

	// The minimum number of pods to kill in a test step
	MinPodsToKill int `json:"minPodsToKill"`
	// The maximum number of pods to kill in a test step
	MaxPodsToKill int `json:"maxPodsToKill"`

	// The minimum sleep time in a test step
	MinSleepTime int `json:"minSleepTime"`
	// The maximum sleep time in a test step
	MaxSleepTime int `json:"maxSleepTime"`

	// The minimum number of subclusters for a scaling test step
	MinSubclusters int `json:"minSubclusters"`
	// The maximum number of subclusters for a scaling test step
	MaxSubclusters int `json:"maxSubclusters"`
	// The minimum number of pods, across all subclusters, for a scaling test step
	MinPods int `json:"minPods"`
	// The maximum number of pods, across all subclusters, for a scaling test step
	MaxPods int `json:"maxPods"`
}

// Locations contains various paths needed to run this program
type Locations struct {
	// The directory where the test steps will be generated.
	OutputDir string
	// The relative directory to the output directory where the repository
	// script directory is located.
	ScriptsDir string
}
