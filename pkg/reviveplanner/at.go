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

package reviveplanner

import (
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
)

type ATParser struct {
	Database         Database
	CommunalLocation CommunalLocation
	Log              logr.Logger
	ParseComplete    bool
}

// MakeATParser is a factory function for the ClusterConfigParser interface.
// This makes one specific to admintools output.
func MakeATParser(log logr.Logger) ClusterConfigParser {
	return &ATParser{
		Log: log.WithName("ATParser"),
	}
}

// getDataPaths will return the data paths for each node
func (a *ATParser) getDataPaths() []string {
	paths := []string{}
	for i := range a.Database.Nodes {
		paths = append(paths, a.Database.Nodes[i].GetDataPaths()...)
	}
	return paths
}

// getDepotPaths will return the depot paths for each node
func (a *ATParser) getDepotPaths() []string {
	paths := []string{}
	for i := range a.Database.Nodes {
		paths = append(paths, a.Database.Nodes[i].GetDepotPath()...)
	}
	return paths
}

// getCatalogPaths will return the catalog paths that are set for each node.
func (a *ATParser) getCatalogPaths() []string {
	paths := []string{}
	for i := range a.Database.Nodes {
		paths = append(paths, a.Database.Nodes[i].CatalogPath)
	}
	return paths
}

// getNumShards returns the number of shards in the cluster config
func (a *ATParser) getNumShards() (int, error) {
	foundShardCount, err := strconv.Atoi(a.CommunalLocation.NumShards)
	if err != nil {
		return 0, fmt.Errorf("Failed to convert shard in revive --display-only output to int: %s",
			a.CommunalLocation.NumShards)
	}
	return foundShardCount, nil
}

// getDatabaseName returns the name of the database as found in the cluster config
func (a *ATParser) getDatabaseName() string {
	return a.Database.Name
}
