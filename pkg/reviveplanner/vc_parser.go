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
	"encoding/json"
	"path/filepath"

	"github.com/go-logr/logr"
)

type VCParser struct {
	Log logr.Logger
	// SPILLY - I don't like the struct names. Another package perhaps?
	ClusterConfigData VCCluster
}

// The next set of structs define the structure of the cluster_config.json. The
// format of these should only change if a corresponding change is done in the
// server. There are many fields in the cluster_config.json. We elected to only
// include the fields that are needed by the revive planner.

// SPILLY - should these be exported?
// VCCluster is the top level JSON doc for the output for vcluster's ReviveDB
// output when DisplayOnly is used.
type VCCluster struct {
	Database        VCDatabase           `json:"Database"`
	ShardCount      int                  `json:"ShardCount"`
	StorageLoctions []VCStorageLocations `json:"StorageLocation"`
	Nodes           []VCNode             `json:"Node"`
}

// VCDatabase contains database information about the cluster
type VCDatabase struct {
	Name string `json:"name"`
}

// VCStorageLocations has data about a single storage location in the cluster
type VCStorageLocations struct {
	Path  string `json:"path"`
	Usage int    `json:"usage"`
}

// VCNode stores info about a single node in the vertica cluster
type VCNode struct {
	CatalogPath string `json:"catalogPath"`
}

// MakeVCParser is a factory function for the ClusterConfigParser interface.
// This makes one specific to vcluster output.
func MakeVCParser(log logr.Logger) ClusterConfigParser {
	return &VCParser{
		Log: log.WithName("VCParser"),
	}
}

// Parse will parse the output given and populate the ClusterConfigData
func (v *VCParser) Parse(op string) error {
	if err := json.Unmarshal([]byte(op), &v.ClusterConfigData); err != nil {
		return err
	}
	return nil
}

// GetDataPaths will return the data paths for each node
func (v *VCParser) GetDataPaths() []string {
	return v.getPathsByUsage(UsageIsDataTemp)
}

// GetDepotPaths will return the depot paths for each node
func (v *VCParser) GetDepotPaths() []string {
	return v.getPathsByUsage(UsageIsDepot)
}

// getPathsByUsage is a helper to paths for a specific usage type
func (v *VCParser) getPathsByUsage(usage int) []string {
	paths := []string{}
	for i := range v.ClusterConfigData.StorageLoctions {
		if v.ClusterConfigData.StorageLoctions[i].Usage == usage {
			paths = append(paths, v.ClusterConfigData.StorageLoctions[i].Path)
		}
	}
	return paths
}

// GetCatalogPaths will return the catalog paths that are set for each node.
func (v *VCParser) GetCatalogPaths() []string {
	paths := []string{}
	for i := range v.ClusterConfigData.Nodes {
		// The catalog path has a '/Catalog' prefix that we need to remove to be
		// in-sync with the --display-only output from admintools.
		catPath := filepath.Dir(v.ClusterConfigData.Nodes[i].CatalogPath)
		paths = append(paths, catPath)
	}
	return paths
}

// GetNumShards returns the number of shards in the cluster config
func (v *VCParser) GetNumShards() (int, error) {
	// Actual shard count return is always -1 to account for replica shard.
	return v.ClusterConfigData.ShardCount - 1, nil
}

// GetDatabaseName returns the name of the database as found in the cluster config
func (v *VCParser) GetDatabaseName() string {
	return v.ClusterConfigData.Database.Name
}
