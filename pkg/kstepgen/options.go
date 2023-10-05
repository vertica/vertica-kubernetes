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
	// What percentage of time do we kill the operator pod instead of vertica
	// pods.
	PctKillOperator int `json:"pctKillOperator"`

	// Config for each database that you want to test against.
	Databases []DatabaseCfg `json:"databases"`
}

type StepTypeWeights struct {
	Scaling         int `json:"scaling"`
	KillVerticaPod  int `json:"killVerticaPod"`
	KillOperatorPod int `json:"killOperatorPod"`
	Sleep           int `json:"sleep"`
}

const (
	ScalingTestStep = iota
	KillVerticaPodTestStep
	KillOperatorPodTestStep
	SleepTestStep
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

	// How often, as a percentage, that we will wait for the scaling event to
	// finish to completing in a test step. This is represented as a number
	// between 0 and 100. 100 meaning we will add the assertion to the step each
	// time and 0 means we will never add the assertion.
	PctAssertScaling int `json:"pctAssertScaling"`
	// Details about all subclusters we will resize/add/remove
	Subclusters []SubclusterCfg `json:"subclusters"`
}

// SubclusterCfg provides config for a single subcluster
type SubclusterCfg struct {
	// The name of the subcluster
	Name string `json:"name"`
	// The minimum size of the subcluster
	MinSize int `json:"minSize"`
	// The maximum size of the subcluster
	MaxSize int `json:"maxSize"`
	// When true the subcluster is removed if the size is 0
	RemoveWhenZero bool `json:"removeWhenZero,omitempty"`
	// True if this subcluster should be a primary one
	IsPrimary bool `json:"isPrimary"`
}

// Locations contains various paths needed to run this program
type Locations struct {
	// The directory where the test steps will be generated.
	OutputDir string
	// The relative directory to the output directory where the repository
	// script directory is located.
	ScriptsDir string
}
