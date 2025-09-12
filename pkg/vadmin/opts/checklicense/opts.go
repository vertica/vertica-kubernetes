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

package checklicense

type Parms struct {
	InitiatorIPs []string
	LicenseFile  string
}

// Option is a function that configures a Parms instance.
type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (p *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(p)
	}
}

func WithLicenseFile(licenseFile string) Option {
	return func(p *Parms) {
		p.LicenseFile = licenseFile
	}
}

func WithInitiators(podIPs []string) Option {
	return func(p *Parms) {
		p.InitiatorIPs = podIPs
	}
}
