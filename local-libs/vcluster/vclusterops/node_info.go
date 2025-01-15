/*
 (c) Copyright [2023-2024] Open Text.
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

package vclusterops

import mapset "github.com/deckarep/golang-set/v2"

// NodeInfo represents information to identify a node.
type NodeInfo struct {
	Address     string `json:"address"`
	Name        string `json:"name"` // vnode name, e.g., v_dbname_node0001
	State       string `json:"state"`
	CatalogPath string `json:"catalog_path"`
	Subcluster  string `json:"subcluster"`
	Sandbox     string `json:"sandbox"`
	IsPrimary   bool   `json:"is_primary"`
	Version     string `json:"version"`
}

// NodeInfo does not contain Eon specific information
type NodeInfoEnterprise struct {
	Address     string `json:"address"`
	Name        string `json:"name"` // vnode name, e.g., v_dbname_node0001
	State       string `json:"state"`
	CatalogPath string `json:"catalog_path"`
	Version     string `json:"version"`
}

type nodesInfo struct {
	NodeList []NodeInfo `json:"node_list"`
}

// findHosts looks for hosts in a list of NodesInfo.
// If found, return true; if not found, return false.
func (info *nodesInfo) findHosts(hosts []string) bool {
	inputHostSet := mapset.NewSet(hosts...)

	nodeAddrSet := mapset.NewSet[string]()
	for _, n := range info.NodeList {
		nodeAddrSet.Add(n.Address)
	}

	return nodeAddrSet.Intersect(inputHostSet).Cardinality() > 0
}
