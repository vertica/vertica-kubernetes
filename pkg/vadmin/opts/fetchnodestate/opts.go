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

package fetchnodestate

import (
	"k8s.io/apimachinery/pkg/types"
)

// Parms holds all of the options for FetchNodeState API call.
type Parms struct {
	DBName      string
	Initiator   types.NamespacedName
	InitiatorIP string
}

// Host has information about a single host to get state for
type Host struct {
	VNode string // vertica node name (e.g. v_vertdb_node0001)
	IP    string // Last known IP address
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
		s.Initiator = nm
		s.InitiatorIP = ip
	}
}
