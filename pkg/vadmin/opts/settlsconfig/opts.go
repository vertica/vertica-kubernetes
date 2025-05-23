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

package settlsconfig

type Parms struct {
	InitiatorIP               string
	ClientServerTLSMode       string
	HTTPSTLSMode              string
	ClientServerTLSSecretName string
	HTTPSTLSSecretName        string
	Namespace                 string
}

// Option is a function that configures a Parms instance.
type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (p *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(p)
	}
}

// WithClientServerTLSSecretName sets the ClientServerTLSSecretName field of the Parms struct.
func WithClientServerTLSSecretName(secret string) Option {
	return func(p *Parms) {
		p.ClientServerTLSSecretName = secret
	}
}

// WithHTTPSTLSMode sets the HTTPSTLSMode field of the Parms struct.
func WithHTTPSTLSMode(tlsMode string) Option {
	return func(p *Parms) {
		p.HTTPSTLSMode = tlsMode
	}
}

// WithHTTPSTLSSecretName sets the HTTPSTLSSecretName field of the Parms struct.
func WithHTTPSTLSSecretName(secret string) Option {
	return func(p *Parms) {
		p.HTTPSTLSSecretName = secret
	}
}

// WithClientServerTLSMode sets the ClientServerTLSMode field of the Parms struct.
func WithClientServerTLSMode(tlsMode string) Option {
	return func(p *Parms) {
		p.ClientServerTLSMode = tlsMode
	}
}

// WithNamespace sets the Namespace field of the Parms struct.
func WithNamespace(namespace string) Option {
	return func(p *Parms) {
		p.Namespace = namespace
	}
}

// WithInitiatorIP sets the InitiatorIP field of the Parms struct.
func WithInitiatorIP(initiatorIP string) Option {
	return func(p *Parms) {
		p.InitiatorIP = initiatorIP
	}
}
