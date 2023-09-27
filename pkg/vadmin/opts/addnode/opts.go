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

package addnode

import (
	"k8s.io/apimachinery/pkg/types"
)

// Parms holds all of the option for a revive DB invocation.
type Parms struct {
	InitiatorName     types.NamespacedName
	InitiatorIP       string
	Hosts             []string
	Subcluster        string
	ExpectedNodeNames []string
}

type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (s *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(s)
	}
}

func WithInitiator(nm types.NamespacedName, ip string) Option {
	return func(s *Parms) {
		s.InitiatorName = nm
		s.InitiatorIP = ip
	}
}

func WithHost(fqdn string) Option {
	return func(s *Parms) {
		if s.Hosts == nil {
			s.Hosts = make([]string, 0)
		}
		s.Hosts = append(s.Hosts, fqdn)
	}
}

func WithSubcluster(subcluster string) Option {
	return func(s *Parms) {
		s.Subcluster = subcluster
	}
}

func WithExpecteNodeNames(nodeNames []string) Option {
	return func(s *Parms) {
		s.ExpectedNodeNames = nodeNames
	}
}
