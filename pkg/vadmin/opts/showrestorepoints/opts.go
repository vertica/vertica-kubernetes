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

package showrestorepoints

import (
	vops "github.com/vertica/vcluster/vclusterops"
	"k8s.io/apimachinery/pkg/types"
)

// Parms holds all of the option for a restore point invocation.
type Parms struct {
	InitiatorName       types.NamespacedName
	InitiatorIP         string
	Hosts               []string
	CommunalPath        string
	ConfigurationParams map[string]string
	FilterOptions       vops.ShowRestorePointFilterOptions
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

func WithCommunalPath(path string) Option {
	return func(s *Parms) {
		s.CommunalPath = path
	}
}

func WithConfigurationParams(parms map[string]string) Option {
	return func(s *Parms) {
		s.ConfigurationParams = parms
	}
}

func WithArchiveNameFilter(archiveName string) Option {
	return func(s *Parms) {
		s.FilterOptions.ArchiveName = archiveName
	}
}

func WithStartTimestampFilter(startTimestamp string) Option {
	return func(s *Parms) {
		s.FilterOptions.StartTimestamp = startTimestamp
	}
}

func WithEndTimestampFilter(endTimestamp string) Option {
	return func(s *Parms) {
		s.FilterOptions.EndTimestamp = endTimestamp
	}
}
