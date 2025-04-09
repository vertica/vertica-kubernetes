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

package rotatehttpscerts

// Params holds all of the option for nma cert rotation.
type Params struct {
	// name of the secret containing key data
	KeySecretName string `json:"key_secret_name"` // required
	// config used by the config manager to extract key data from secret
	KeyConfig string `json:"key_config,omitempty"`
	// name of the secret containing certificate data
	CertSecretName string `json:"cert_secret_name"` // required
	// config used by the config manager to extract cert data from secret
	CertConfig string `json:"cert_config,omitempty"`
	// name of the secret containing ca certificate data
	CACertSecretName string `json:"ca_cert_secret_name"` // required
	// config used by the config manager to extract ca cert data from secret
	CACertConfig string `json:"ca_cert_config,omitempty"`
	// if changing tls mode, vertica server tls mode, e.g. "verify_full"
	TLSMode string `json:"tlsmode,omitempty"`
	// TLS Key (PEM bytes)
	NewKey string
	// TLS Certificate (PEM bytes)
	NewCert string
	// TLS CA Certificate (PEM bytes)
	NewCaCert   string
	InitiatorIP string
}

type Option func(*Params)

// Make will fill in the Params based on the options chosen
func (s *Params) Make(opts ...Option) {
	for _, opt := range opts {
		opt(s)
	}
}

func WithKey(keySecretName, keyConfig string) Option {
	return func(s *Params) {
		s.KeySecretName = keySecretName
		s.KeyConfig = keyConfig
	}
}

func WithCert(certSecretName, certConfig string) Option {
	return func(s *Params) {
		s.CertSecretName = certSecretName
		s.CertConfig = certConfig
	}
}

func WithCaCert(caCertSecretName, caCertConfig string) Option {
	return func(s *Params) {
		s.CACertSecretName = caCertSecretName
		s.CACertConfig = caCertConfig
	}
}

func WithTLSMode(tlsMode string) Option {
	return func(s *Params) {
		s.TLSMode = tlsMode
	}
}

func WithPollingKey(newKey string) Option {
	return func(s *Params) {
		s.NewKey = newKey
	}
}

func WithPollingCert(newCert string) Option {
	return func(s *Params) {
		s.NewCert = newCert
	}
}

func WithPollingCaCert(newCaCert string) Option {
	return func(s *Params) {
		s.NewCaCert = newCaCert
	}
}

func WithInitiator(ip string) Option {
	return func(s *Params) {
		s.InitiatorIP = ip
	}
}
