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

package getconfigparameter

// Params holds all of the options for a get config parameter invocation.
type Params struct {
	UserName        string
	InitiatorIP     string
	Sandbox         string
	ConfigParameter string
	Level           string
}

// Option is a function that configures a Params instance.
type Option func(*Params)

// Make will fill in the Params based on the options chosen
func (p *Params) Make(opts ...Option) {
	for _, opt := range opts {
		opt(p)
	}
}

// WithUserName sets the UserName field of the Params struct.
func WithUserName(userName string) Option {
	return func(p *Params) {
		p.UserName = userName
	}
}

// WithInitiatorIP sets the InitiatorIP field of the Params struct.
func WithInitiatorIP(initiatorIP string) Option {
	return func(p *Params) {
		p.InitiatorIP = initiatorIP
	}
}

// WithSandbox sets the Sandbox field of the Params struct.
func WithSandbox(sandbox string) Option {
	return func(p *Params) {
		p.Sandbox = sandbox
	}
}

// WithConfigParameter sets the ConfigParameter field of the Params struct.
func WithConfigParameter(configParameter string) Option {
	return func(p *Params) {
		p.ConfigParameter = configParameter
	}
}

// WithLevel sets the Level field of the Params struct.
func WithLevel(level string) Option {
	return func(p *Params) {
		p.Level = level
	}
}
