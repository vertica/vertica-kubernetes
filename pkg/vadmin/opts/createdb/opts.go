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

package createdb

import (
	"k8s.io/apimachinery/pkg/types"
)

// Parms holds all of the option for a create DB invocation.
type Parms struct {
	Initiator             types.NamespacedName
	PodNames              []types.NamespacedName
	Hosts                 []string
	PostDBCreateSQLFile   string
	CatalogPath           string
	DepotPath             string
	DataPath              string
	DBName                string
	LicensePath           string
	CommunalPath          string
	CommunalStorageParams string
	ConfigurationParams   map[string]string
	ShardCount            int
	SkipPackageInstall    bool
}

type Option func(*Parms)

// Make will fill in the Parms based on the options chosen
func (s *Parms) Make(opts ...Option) {
	for _, opt := range opts {
		opt(s)
	}
}

func WithInitiator(nm types.NamespacedName) Option {
	return func(s *Parms) {
		s.Initiator = nm
	}
}

func WithPods(nm []types.NamespacedName) Option {
	return func(s *Parms) {
		s.PodNames = nm
	}
}

func WithHosts(hosts []string) Option {
	return func(s *Parms) {
		s.Hosts = hosts
	}
}

func WithPostDBCreateSQLFile(file string) Option {
	return func(s *Parms) {
		s.PostDBCreateSQLFile = file
	}
}

func WithCatalogPath(path string) Option {
	return func(s *Parms) {
		s.CatalogPath = path
	}
}

func WithDepotPath(path string) Option {
	return func(s *Parms) {
		s.DepotPath = path
	}
}

func WithDataPath(path string) Option {
	return func(s *Parms) {
		s.DataPath = path
	}
}

func WithDBName(name string) Option {
	return func(s *Parms) {
		s.DBName = name
	}
}

func WithLicensePath(path string) Option {
	return func(s *Parms) {
		s.LicensePath = path
	}
}

func WithCommunalPath(path string) Option {
	return func(s *Parms) {
		s.CommunalPath = path
	}
}

func WithCommunalStorageParams(path string) Option {
	return func(s *Parms) {
		s.CommunalStorageParams = path
	}
}

func WithConfigurationParams(parms map[string]string) Option {
	return func(s *Parms) {
		s.ConfigurationParams = parms
	}
}

func WithShardCount(shards int) Option {
	return func(s *Parms) {
		s.ShardCount = shards
	}
}

func WithSkipPackageInstall() Option {
	return func(s *Parms) {
		s.SkipPackageInstall = true
	}
}
