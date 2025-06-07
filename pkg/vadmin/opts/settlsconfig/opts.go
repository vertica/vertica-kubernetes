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
	InitiatorIP    string
	TLSMode        string
	TLSSecretName  string
	HTTPSTLSConfig bool
	Namespace      string
}

// Option is a function that configures a Parms instance.
type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (p *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(p)
	}
}

// WithTLSSecretName sets the secret name field of the Parms struct.
func WithTLSSecretName(secret string) Option {
	return func(p *Parms) {
		p.TLSSecretName = secret
	}
}

// WithHTTPSTLSMode sets the HTTPSTLSMode field of the Parms struct.
func WithTLSMode(tlsMode string) Option {
	return func(p *Parms) {
		p.TLSMode = tlsMode
	}
}

// WithTLSConfig sets TLSConfig field of the Parms struct.
func WithHTTPSTLSConfig(isHTTPSTLSConfig bool) Option {
	return func(p *Parms) {
		p.HTTPSTLSConfig = isHTTPSTLSConfig
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
