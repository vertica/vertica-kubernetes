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

package restartnode

import (
	"k8s.io/apimachinery/pkg/types"
)

// Parms holds all of the option for a revive DB invocation.
type Parms struct {
	InitiatorName types.NamespacedName
	InitiatorIP   string
	RestartHosts  map[string]string // All of the hosts we want to restart. This is a map of vnodes to their IP.
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

func WithHost(vnode, ip string) Option {
	return func(s *Parms) {
		if s.RestartHosts == nil {
			s.RestartHosts = make(map[string]string)
		}
		s.RestartHosts[vnode] = ip
	}
}
