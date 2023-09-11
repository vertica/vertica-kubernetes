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

package vcparser

import (
	"encoding/json"
	"path/filepath"

	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner/util"
)

type Parser struct {
	Cluster Cluster
}

// The next set of structs define the structure of the cluster_config.json. The
// format of these should only change if a corresponding change is done in the
// server. There are many fields in the cluster_config.json. We elected to only
// include the fields that are needed by the revive planner.

// Cluster is the top level JSON doc for the output for vcluster's ReviveDB
// output when DisplayOnly is used.
type Cluster struct {
	Database         Database           `json:"Database"`
	ShardCount       int                `json:"ShardCount"`
	StorageLocations []StorageLocations `json:"StorageLocation"`
	Nodes            []Node             `json:"Node"`
}

// Database contains database information about the cluster
type Database struct {
	Name string `json:"name"`
}

// StorageLocations has data about a single storage location in the cluster
type StorageLocations struct {
	Path  string `json:"path"`
	Usage int    `json:"usage"`
}

// Node stores info about a single node in the vertica cluster
type Node struct {
	CatalogPath string `json:"catalogPath"`
}

// Parse will parse the output given and populate the ClusterConfigData
func (v *Parser) Parse(op string) error {
	return json.Unmarshal([]byte(op), &v.Cluster)
}

// GetDataPaths will return the data paths for each node
func (v *Parser) GetDataPaths() []string {
	return v.getPathsByUsage(util.UsageIsDataTemp)
}

// GetDepotPaths will return the depot paths for each node
func (v *Parser) GetDepotPaths() []string {
	return v.getPathsByUsage(util.UsageIsDepot)
}

// getPathsByUsage is a helper to paths for a specific usage type
func (v *Parser) getPathsByUsage(usage int) []string {
	paths := []string{}
	for i := range v.Cluster.StorageLocations {
		if v.Cluster.StorageLocations[i].Usage == usage {
			paths = append(paths, v.Cluster.StorageLocations[i].Path)
		}
	}
	return paths
}

// GetCatalogPaths will return the catalog paths that are set for each node.
func (v *Parser) GetCatalogPaths() []string {
	paths := []string{}
	for i := range v.Cluster.Nodes {
		// The catalog path has a '/Catalog' prefix that we need to remove to be
		// in-sync with the --display-only output from admintools.
		catPath := filepath.Dir(v.Cluster.Nodes[i].CatalogPath)
		paths = append(paths, catPath)
	}
	return paths
}

// GetNumShards returns the number of shards in the cluster config
func (v *Parser) GetNumShards() (int, error) {
	// Actual shard count return is always -1 to account for replica shard.
	return v.Cluster.ShardCount - 1, nil
}

// GetDatabaseName returns the name of the database as found in the cluster config
func (v *Parser) GetDatabaseName() string {
	return v.Cluster.Database.Name
}
