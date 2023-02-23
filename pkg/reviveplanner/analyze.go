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
	"sort"
	"strconv"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

// IsCompatible will check the vdb and extracted revive info to see if
// everything is compatible. Returns a failure if an error is detected.
func (a *ATPlanner) IsCompatible() (string, bool) {
	if err := a.checkForCompatiblePaths(); err != nil {
		// Extract out the error message and return that.
		return err.Error(), false
	}
	return "", true
}

// checkForCompatiblePaths does the heavy lifting of checking for compatible
// paths. It returns an error if the paths aren't compatible.
func (a *ATPlanner) checkForCompatiblePaths() error {
	// To see if the revive is compatible, we are going to check each of the
	// paths of all the nodes. The prefix of each path needs to be the same. The
	// operator assumes that all paths are homogeneous across all vertica hosts.
	if _, err := a.getCommonPath(a.getDepotPaths(), ""); err != nil {
		return err
	}

	catPath, err := a.getCommonPath(a.getCatalogPaths(), "")
	if err != nil {
		return err
	}

	// We tolerate a mix of paths for the data, as long as it matches the
	// catalog path. This exists due to a bug in revive where the constructed
	// admintools.conf has erronously set the data path to match the catalog
	// path. This isn't a problem for existing nodes because the vertica catalog
	// still has the correct path for data.  But if a scale-out occurs with the
	// bad admintools.conf, new nodes will have a data path that matches the
	// catalog path.
	_, err = a.getCommonPath(a.getDataPaths(), catPath)
	return err
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

	catPath, err := a.getCommonPath(a.getCatalogPaths(), "")
	if err != nil {
		return
	}
	if catPath != vdb.Spec.Local.GetCatalogPath() {
		a.logPathChange("catalog", vdb.Spec.Local.GetCatalogPath(), catPath)
		vdb.Spec.Local.CatalogPath = catPath
		updated = true
	}

	// Generally the data path should be the same across all hosts. But it's
	// possible for some nodes to have different one -- as long as the different
	// path matches the catalog path.
	dataPath, err := a.getCommonPath(a.getDataPaths(), catPath)
	if err != nil {
		return
	}
	if dataPath != vdb.Spec.Local.DataPath {
		a.logPathChange("data", vdb.Spec.Local.DataPath, dataPath)
		vdb.Spec.Local.DataPath = dataPath
		updated = true
	}

	depotPath, err := a.getCommonPath(a.getDepotPaths(), "")
	if err != nil {
		return
	}
	if depotPath != vdb.Spec.Local.DepotPath {
		a.logPathChange("depot", vdb.Spec.Local.DepotPath, depotPath)
		vdb.Spec.Local.DepotPath = depotPath
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
		paths = append(paths, a.Database.Nodes[i].GetDataPaths()...)
	}
	return paths
}

// getDepotPaths will return the depot paths for each node
func (a *ATPlanner) getDepotPaths() []string {
	paths := []string{}
	for i := range a.Database.Nodes {
		paths = append(paths, a.Database.Nodes[i].GetDepotPath()...)
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
// all of them. The allowedOutlier parameter, if set, will allow some deviation
// among the paths as long is it matches the outlier.
func (a *ATPlanner) getCommonPath(paths []string, allowedOutlier string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no paths passed in")
	}
	paths = a.removeOutliers(paths, allowedOutlier)
	if len(paths) == 0 {
		return allowedOutlier, nil
	}
	if len(paths) == 1 {
		// If the path has a vnode in it, strip that part off
		if fp, ok := a.extractPathPrefixFromVNodePath(paths[0]); ok {
			return fp, nil
		}
		return strings.TrimSuffix(paths[0], "/"), nil
	}

	// We want to find the common prefix of all of the paths. If we sort the
	// list, we only need to look at the first and list items.
	sort.Slice(paths, func(i, j int) bool {
		return paths[i] < paths[j]
	})
	const beg = 0
	end := len(paths) - 1

	// Find a common prefix between the first and last paths.  We only match
	// complete directories. So, we need to keep track of the current directory
	// as we go.
	var fullPath strings.Builder
	var curDir strings.Builder
	commonLen := min(len(paths[beg]), len(paths[end]))
	for i := 0; i < commonLen; i++ {
		if paths[beg][i] == paths[end][i] {
			cur := paths[beg][i]
			if cur == '/' {
				fullPath.WriteString(curDir.String())
				fullPath.WriteByte(cur)
				curDir.Reset()
			} else {
				curDir.WriteByte(cur)
			}
		} else {
			break
		}
	}
	if fullPath.Len() <= 1 {
		return "", fmt.Errorf("multiple vertica hosts don't have common paths: %s and %s", paths[beg], paths[end])
	}

	// If the common path ended with a partial vnode directory, then trim off
	// the database directory immediately preceding it.
	fp := a.trimOffDatabaseDir(fullPath.String(), curDir.String())

	// Remove any trailing '/' chars
	return strings.TrimSuffix(fp, "/"), nil
}

// removeOutliers builds a path list with any outliers removed
func (a *ATPlanner) removeOutliers(paths []string, allowedOutlier string) []string {
	p := []string{}
	for i := range paths {
		if paths[i] == allowedOutlier {
			continue
		}
		if pathPrefix, ok := a.extractPathPrefixFromVNodePath(paths[i]); ok && pathPrefix == allowedOutlier {
			continue
		}
		p = append(p, paths[i])
	}
	return p
}

// extractPathPrefixFromVNodePath will extract out the prefix of a vertica POSIX path. This
// path could be catalog, depot or data path.
func (a *ATPlanner) extractPathPrefixFromVNodePath(path string) (string, bool) {
	// Path will come in the form: <prefix>/<dbname>/v_<dbname>_<nodenum>_<pathType>
	// This function will return <prefix>.
	r := regexp.MustCompile(fmt.Sprintf(`(.*)/%s/v_%s_node[0-9]{4}_`, a.Database.Name, strings.ToLower(a.Database.Name)))
	m := r.FindStringSubmatch(path)
	const ExpectedMatches = 2
	if len(m) < ExpectedMatches {
		return "", false
	}
	return m[1], true
}

func min(a, b int) int {
	if a > b {
		return b
	}
	return a
}

func (a *ATPlanner) trimOffDatabaseDir(fullPath, partialDir string) string {
	dbNameSuffix := fmt.Sprintf("/%s/", a.Database.Name)
	if strings.HasPrefix(partialDir, fmt.Sprintf("v_%s_node", strings.ToLower(a.Database.Name))) && strings.HasSuffix(fullPath, dbNameSuffix) {
		return strings.TrimSuffix(fullPath, dbNameSuffix)
	}
	return fullPath
}
