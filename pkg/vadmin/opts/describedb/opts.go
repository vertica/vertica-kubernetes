/*
 (c) Copyright [2021-2023] Open Text.
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

package describedb

import (
	"k8s.io/apimachinery/pkg/types"
)

// Parms holds all of the option for an invocation to describe the database in
// communal storage.
type Parms struct {
	Initiator             types.NamespacedName
	DBName                string
	CommunalPath          string
	CommunalStorageParams string
	ConfigurationParams   map[string]string
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

func WithDBName(name string) Option {
	return func(s *Parms) {
		s.DBName = name
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
