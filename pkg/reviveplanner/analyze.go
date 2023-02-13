/*
 (c) Copyright [2021-2023] Micro Focus or one of its affiliates.
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
	"regexp"
	"strconv"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

// IsCompatible will check the vdb and extracted revive info to see if
// everything is compatible. Returns a failure if an error is detected.
func (a *ATPlanner) IsCompatible() (string, bool) {
	// To see if the revive is compatible, we are going to check each of the
	// paths of all the nodes. The prefix of each path needs to be the same. The
	// operator assumes that all paths are homogeneous across all vertica hosts.
	pathFuncs := []func() []string{
		a.getDataPaths,
		a.getDepotPaths,
		a.getCatalogPaths,
	}
	for i := range pathFuncs {
		_, err := a.getCommonPath(pathFuncs[i]())
		if err != nil {
			// Extract out the error message and return that.
			return err.Error(), false
		}
	}
	return "", true
}

// ApplyChanges will update the input vdb based on things it found during
// analysis. Return true if the vdb was updated.
func (a *ATPlanner) ApplyChanges(vdb *vapi.VerticaDB) (updated bool, err error) {
	foundShardCount, err := strconv.Atoi(a.CommunalLocation.NumShards)
	if err != nil {
		a.Log.Info("Failed to convert shard in revive --display-only output to int",
			"num_shards", a.CommunalLocation.NumShards)
		// We won't be able to validate/update the shard count. Ignore the error and continue.
	} else if foundShardCount != vdb.Spec.ShardCount {
		a.Log.Info("Shard count changing to match revive output",
			"oldShardCount", vdb.Spec.ShardCount, "newShardCount", foundShardCount)
		vdb.Spec.ShardCount = foundShardCount
		updated = true
	}

	dataPath, err := a.getCommonPath(a.getDataPaths())
	if err != nil {
		return
	}
	if dataPath != vdb.Spec.Local.DataPath {
		a.logPathChange("data", vdb.Spec.Local.DataPath, dataPath)
		vdb.Spec.Local.DataPath = dataPath
		updated = true
	}

	depotPath, err := a.getCommonPath(a.getDepotPaths())
	if err != nil {
		return
	}
	if depotPath != vdb.Spec.Local.DepotPath {
		a.logPathChange("depot", vdb.Spec.Local.DepotPath, depotPath)
		vdb.Spec.Local.DepotPath = depotPath
		updated = true
	}

	catPath, err := a.getCommonPath(a.getCatalogPaths())
	if err != nil {
		return
	}
	if catPath != vdb.Spec.Local.GetCatalogPath() {
		a.logPathChange("catalog", vdb.Spec.Local.GetCatalogPath(), catPath)
		vdb.Spec.Local.CatalogPath = catPath
		updated = true
	}

	return updated, err
}

// logPathChange will add a log entry for a change to one of the vdb path changes
func (a *ATPlanner) logPathChange(pathType, oldPath, newPath string) {
	a.Log.Info(fmt.Sprintf("%s path has to change to match revive output", pathType),
		"oldPath", oldPath, "newPath", newPath)
}

// getDataPaths will return the data paths for each node
func (a *ATPlanner) getDataPaths() []string {
	paths := []string{}
	for i := range a.Database.Nodes {
		p, ok := a.Database.Nodes[i].GetDataPath()
		if !ok {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

// getDepotPaths will return the depot paths for each node
func (a *ATPlanner) getDepotPaths() []string {
	paths := []string{}
	for i := range a.Database.Nodes {
		p, ok := a.Database.Nodes[i].GetDepotPath()
		if !ok {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

// getCatalogPaths will return the catalog paths that are set for each node.
func (a *ATPlanner) getCatalogPaths() []string {
	paths := []string{}
	for i := range a.Database.Nodes {
		paths = append(paths, a.Database.Nodes[i].CatalogPath)
	}
	return paths
}

// getCommonPath will look at a slice of paths, and return the common prefix for
// all of them.
func (a *ATPlanner) getCommonPath(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no paths passed in")
	}
	commonPath := ""
	for i := range paths {
		p, err := a.extractPathPrefix(paths[i])
		if err != nil {
			return "", err
		}
		if commonPath == "" {
			commonPath = p
		} else if commonPath != p {
			return "", fmt.Errorf("multiple vertica hosts don't have common paths: %s and %s", commonPath, p)
		}
	}
	return commonPath, nil
}

// extractPathPrefix will extract out the prefix of a vertica POSIX path. This
// path could be catalog, depot or data path.
func (a *ATPlanner) extractPathPrefix(path string) (string, error) {
	// Path will come in the form: <prefix>/<dbname>/v_<dbname>_<nodenum>_<pathType>
	// This function will return <prefix>.
	r := regexp.MustCompile(fmt.Sprintf(`(.*)/%s/v_%s_node[0-9]{4}_`, a.Database.Name, strings.ToLower(a.Database.Name)))
	m := r.FindStringSubmatch(path)
	const ExpectedMatches = 2
	if len(m) < ExpectedMatches {
		return "", fmt.Errorf("path '%s' is not a valid vertica path", path)
	}
	return m[1], nil
}
