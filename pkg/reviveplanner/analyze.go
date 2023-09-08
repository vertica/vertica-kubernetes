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
	"regexp"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

type Planner struct {
	Log    logr.Logger
	Parser ClusterConfigParser
}

func MakePlanner(log logr.Logger, parser ClusterConfigParser) *Planner {
	return &Planner{
		Log:    log,
		Parser: parser,
	}
}

// IsCompatible will check the vdb and extracted revive info to see if
// everything is compatible. Returns a failure if an error is detected.
func (p *Planner) IsCompatible() (string, bool) {
	if err := p.checkForCompatiblePaths(); err != nil {
		// Extract out the error message and return that.
		return err.Error(), false
	}
	return "", true
}

// checkForCompatiblePaths does the heavy lifting of checking for compatible
// paths. It returns an error if the paths aren't compatible.
func (p *Planner) checkForCompatiblePaths() error {
	// To see if the revive is compatible, we are going to check each of the
	// paths of all the nodes. The prefix of each path needs to be the same. The
	// operator assumes that all paths are homogeneous across all vertica hosts.
	if _, err := p.getCommonPath(p.Parser.GetDepotPaths(), ""); err != nil {
		return err
	}

	catPath, err := p.getCommonPath(p.Parser.GetCatalogPaths(), "")
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
	_, err = p.getCommonPath(p.Parser.GetDataPaths(), catPath)
	return err
}

// ApplyChanges will update the input vdb based on things it found during
// analysis. Return true if the vdb was updated.
func (p *Planner) ApplyChanges(vdb *vapi.VerticaDB) (updated bool, err error) {
	foundShardCount, err := p.Parser.GetNumShards()
	if err != nil {
		p.Log.Info("Failed to convert shard in cluster config", "err", err)
		// We won't be able to validate/update the shard count. Ignore the error and continue.
	} else if foundShardCount != vdb.Spec.ShardCount {
		p.Log.Info("Shard count changing to match revive output",
			"oldShardCount", vdb.Spec.ShardCount, "newShardCount", foundShardCount)
		vdb.Spec.ShardCount = foundShardCount
		updated = true
	}

	catPath, err := p.getCommonPath(p.Parser.GetCatalogPaths(), "")
	if err != nil {
		return
	}
	if catPath != vdb.Spec.Local.GetCatalogPath() {
		p.logPathChange("catalog", vdb.Spec.Local.GetCatalogPath(), catPath)
		vdb.Spec.Local.CatalogPath = catPath
		updated = true
	}

	// Generally the data path should be the same across all hosts. But it's
	// possible for some nodes to have different one -- as long as the different
	// path matches the catalog path.
	dataPath, err := p.getCommonPath(p.Parser.GetDataPaths(), catPath)
	if err != nil {
		return
	}
	if dataPath != vdb.Spec.Local.DataPath {
		p.logPathChange("data", vdb.Spec.Local.DataPath, dataPath)
		vdb.Spec.Local.DataPath = dataPath
		updated = true
	}

	depotPath, err := p.getCommonPath(p.Parser.GetDepotPaths(), "")
	if err != nil {
		return
	}
	if depotPath != vdb.Spec.Local.DepotPath {
		p.logPathChange("depot", vdb.Spec.Local.DepotPath, depotPath)
		vdb.Spec.Local.DepotPath = depotPath
		updated = true
	}
	if vdb.IsDepotVolumeEmptyDir() && !vdb.Spec.Local.IsDepotPathUnique() {
		p.Log.Info("depot path not unique, depotVolume has to change to PersistentVolume")
		// Because when depotVolume is EmptyDir, we cannot have depot path
		// equal to catalog or data path. We will instead have PersistentVolume
		// as depot volume.
		vdb.Spec.Local.DepotVolume = vapi.PersistentVolume
		updated = true
	}

	return updated, err
}

// logPathChange will add a log entry for a change to one of the vdb path changes
func (p *Planner) logPathChange(pathType, oldPath, newPath string) {
	p.Log.Info(fmt.Sprintf("%s path has to change to match revive output", pathType),
		"oldPath", oldPath, "newPath", newPath)
}

// getCommonPath will look at a slice of paths, and return the common prefix for
// all of them. The allowedOutlier parameter, if set, will allow some deviation
// among the paths as long is it matches the outlier.
func (p *Planner) getCommonPath(paths []string, allowedOutlier string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("no paths passed in")
	}
	paths = p.removeOutliers(paths, allowedOutlier)
	if len(paths) == 0 {
		return allowedOutlier, nil
	}
	if len(paths) == 1 {
		// If the path has a vnode in it, strip that part off
		if fp, ok := p.extractPathPrefixFromVNodePath(paths[0]); ok {
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
	fp := p.trimOffDatabaseDir(fullPath.String(), curDir.String())

	// Remove any trailing '/' chars
	return strings.TrimSuffix(fp, "/"), nil
}

// removeOutliers builds a path list with any outliers removed
func (p *Planner) removeOutliers(paths []string, allowedOutlier string) []string {
	outPaths := []string{}
	for i := range paths {
		if paths[i] == allowedOutlier {
			continue
		}
		if pathPrefix, ok := p.extractPathPrefixFromVNodePath(paths[i]); ok && pathPrefix == allowedOutlier {
			continue
		}
		outPaths = append(outPaths, paths[i])
	}
	return outPaths
}

// extractPathPrefixFromVNodePath will extract out the prefix of a vertica POSIX path. This
// path could be catalog, depot or data path.
func (p *Planner) extractPathPrefixFromVNodePath(path string) (string, bool) {
	// Path will come in the form: <prefix>/<dbname>/v_<dbname>_<nodenum>_<pathType>
	// This function will return <prefix>.
	dbName := p.Parser.GetDatabaseName()
	r := regexp.MustCompile(fmt.Sprintf(`(.*)/%s/v_%s_node[0-9]{4}_`, dbName, strings.ToLower(dbName)))
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

func (p *Planner) trimOffDatabaseDir(fullPath, partialDir string) string {
	dbName := p.Parser.GetDatabaseName()
	dbNameSuffix := fmt.Sprintf("/%s/", dbName)
	if strings.HasPrefix(partialDir, fmt.Sprintf("v_%s_node", strings.ToLower(dbName))) && strings.HasSuffix(fullPath, dbNameSuffix) {
		return strings.TrimSuffix(fullPath, dbNameSuffix)
	}
	return fullPath
}
