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

package rotatenmacerts

// Params holds all of the option for nma cert rotation.
type Params struct {
	// TLS Key (PEM bytes)
	NewKey string
	// TLS Certificate (PEM bytes)
	NewCert string
	// TLS CA Certificate (PEM bytes)
	NewCaCert string
	Hosts     []string
}

type Option func(*Params)

// Make will fill in the Params based on the options chosen
func (s *Params) Make(opts ...Option) {
	for _, opt := range opts {
		opt(s)
	}
}

func WithKey(newKey string) Option {
	return func(s *Params) {
		s.NewKey = newKey
	}
}

func WithCert(newCert string) Option {
	return func(s *Params) {
		s.NewCert = newCert
	}
}

func WithCaCert(newCaCert string) Option {
	return func(s *Params) {
		s.NewCaCert = newCaCert
	}
}

func WithHosts(hosts []string) Option {
	return func(s *Params) {
		s.Hosts = hosts
	}
}
