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

package pollhttps

import "github.com/vertica/vertica-kubernetes/pkg/tls"

type Parms struct {
	InitiatorIPs         []string
	MainClusterInitiator string
	SyncCatalogRequire   bool
	NewKey               string
	NewCert              string
	NewCaCert            string
}

// Option is a function that configures a Parms instance.
type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (p *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(p)
	}
}

func WithMainClusterHosts(podIP string) Option {
	return func(p *Parms) {
		p.MainClusterInitiator = podIP
	}
}

func WithInitiators(podIPs []string) Option {
	return func(p *Parms) {
		p.InitiatorIPs = podIPs
	}
}

func WithSyncCatalogRequired(syncCatalogRequired bool) Option {
	return func(p *Parms) {
		p.SyncCatalogRequire = syncCatalogRequired
	}
}

func WithNewHTTPSCerts(cert *tls.HTTPSCerts) Option {
	return func(p *Parms) {
		p.NewKey = cert.Key
		p.NewCert = cert.Cert
		p.NewCaCert = cert.CaCert
	}
}
