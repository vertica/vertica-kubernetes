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

package altersc

// Parms holds all of the option for a revive DB invocation.
type Parms struct {
	InitiatorIP string
	// Name of the subcluster to promote or demote in sandbox or main cluster
	SCName string
	// Type of the subcluster to promote or demote
	// Set to primary to demote the subcluster.
	// Set to secondary to promote the subcluster.
	SCType string
	// Name of the sandbox
	// Use this option when promoting or demoting a subcluster in a sandbox.
	// If this option is not set, the subcluster will be promoted or demoted in the main cluster.
	Sandbox string
}

type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (s *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(s)
	}
}

func WithInitiator(ip string) Option {
	return func(s *Parms) {
		s.InitiatorIP = ip
	}
}

func WithSubcluster(scName string) Option {
	return func(s *Parms) {
		s.SCName = scName
	}
}

func WithSubclusterType(scType string) Option {
	return func(s *Parms) {
		s.SCType = scType
	}
}

func WithSandbox(sbName string) Option {
	return func(s *Parms) {
		s.Sandbox = sbName
	}
}
