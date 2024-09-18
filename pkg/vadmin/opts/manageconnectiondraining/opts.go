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

package manageconnectiondraining

import "github.com/vertica/vcluster/vclusterops"

// Params holds all of the option for a connection draining operation
type Params struct {
	InitiatorIP      string
	Sandbox          string
	SCName           string
	Action           vclusterops.ConnectionDrainingAction
	RedirectHostname string
}

type Option func(*Params)

// Make will fill in the Params based on the options chosen
func (s *Params) Make(opts ...Option) {
	for _, opt := range opts {
		opt(s)
	}
}

// WithInitiator sets the InitiatorIP field of the Parms struct.
func WithInitiator(initiatorIP string) Option {
	return func(s *Params) {
		s.InitiatorIP = initiatorIP
	}
}

// WithSandbox sets the Sandbox field of the Parms struct.
func WithSandbox(sandbox string) Option {
	return func(s *Params) {
		s.Sandbox = sandbox
	}
}

// WithSubcluster sets the SCName field of the Parms struct.
func WithSubcluster(sc string) Option {
	return func(s *Params) {
		s.SCName = sc
	}
}

// WithAction sets the Action field of the Parms struct.
func WithAction(action vclusterops.ConnectionDrainingAction) Option {
	return func(s *Params) {
		s.Action = action
	}
}

// WithRedirectHostname sets the RedirectHostname field of the Parms struct.
func WithRedirectHostname(host string) Option {
	return func(s *Params) {
		s.RedirectHostname = host
	}
}
