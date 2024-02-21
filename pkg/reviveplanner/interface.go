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

package reviveplanner

import (
	"github.com/go-logr/logr"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner/atparser"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner/vcparser"
)

// ClusterConfigParser is an interface for parsing the output of the revive
// --display-only command.
type ClusterConfigParser interface {
	// Parse will look at the given output, from revive --display-only, and
	// parse it into Go structs. Accessor functions exist to get at the various
	// states it parses.
	Parse(op string) error

	// Accessor functions for the states that we found while parsing.
	GetDataPaths() []string
	GetDepotPaths() []string
	GetCatalogPaths() []string
	GetNumShards() (int, error)
	GetDatabaseName() string
}

// ClusterConfigParserFactory is a factory function that builds a concrete
// struct that implements the ClusterConfigParser interface.
func ClusterConfigParserFactory(vclusterOps bool, log logr.Logger) ClusterConfigParser {
	if vclusterOps {
		return makeVCParser()
	}
	return makeATParser(log)
}

// makeATParser is a factory function for the ClusterConfigParser interface.
// This makes one specific to admintools output.
func makeATParser(log logr.Logger) ClusterConfigParser {
	return &atparser.Parser{
		Log: log.WithName("ATParser"),
	}
}

// makeVCParser is a factory function for the ClusterConfigParser interface.
// This makes one specific to vcluster output.
func makeVCParser() ClusterConfigParser {
	return &vcparser.Parser{}
}
