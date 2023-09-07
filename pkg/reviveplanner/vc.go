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

import "github.com/go-logr/logr"

type VCParser struct {
	Log logr.Logger
	// SPILLY - create JSON for fields that are specific from vcluster output
}

// MakeVCParser is a factory function for the ClusterConfigParser interface.
// This makes one specific to vcluster output.
func MakeVCParser(log logr.Logger) ClusterConfigParser {
	return &VCParser{
		Log: log.WithName("VCParser"),
	}
}

func (v *VCParser) Parse(op string) error {
	// SPILLY - implement this
	return nil
}

// getDataPaths will return the data paths for each node
func (v *VCParser) getDataPaths() []string {
	// SPILLY - implement this
	return nil
}

// getDepotPaths will return the depot paths for each node
func (v *VCParser) getDepotPaths() []string {
	// SPILLY - implement this
	return nil
}

// getCatalogPaths will return the catalog paths that are set for each node.
func (v *VCParser) getCatalogPaths() []string {
	// SPILLY - implement this
	return nil
}

// getNumShards returns the number of shards in the cluster config
func (v *VCParser) getNumShards() (int, error) {
	// SPILLY - implement this
	return 0, nil
}

// getDatabaseName returns the name of the database as found in the cluster config
func (v *VCParser) getDatabaseName() string {
	// SPILLY - implement this
	return ""
}
