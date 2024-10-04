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
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"golang.org/x/exp/maps"
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
	// verify if we can get any local depot paths
	if _, err := p.getLocalPaths(p.Parser.GetDepotPaths()); err != nil {
		return err
	}

	// verify if we can get any local catalog paths
	paths, err := p.getLocalPaths(p.Parser.GetCatalogPaths())
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return fmt.Errorf("expected 1 catalog path, got multiple: %+v", paths)
	}

	// verify if we can get any local data paths
	_, err = p.getLocalPaths(p.Parser.GetDataPaths())
	return err
}

// ApplyChanges will update the input vdb based on things it found during
// analysis. Return true if the vdb was updated.
func (p *Planner) ApplyChanges(vdb *vapi.VerticaDB) (updated bool, err error) {
	updatedShardCount := false
	foundShardCount, err := p.Parser.GetNumShards()
	if err != nil {
		p.Log.Info("Failed to convert shard in cluster config", "err", err)
		// We won't be able to validate/update the shard count. Ignore the error and continue.
	} else if foundShardCount != vdb.Spec.ShardCount {
		p.Log.Info("Shard count changing to match revive output",
			"oldShardCount", vdb.Spec.ShardCount, "newShardCount", foundShardCount)
		vdb.Spec.ShardCount = foundShardCount
		updatedShardCount = true
	}

	// extraPaths will contain extra local paths that are not in the local.catalogPath, local.dataPath, and local.depotPath.
	extraPaths := make(map[string]struct{})
	updated, err = p.updateLocalPathsInVdb(vdb, extraPaths)
	updated = updated || updatedShardCount
	if err != nil {
		return updated, err
	}

	otherPaths := p.Parser.GetOtherPaths()
	if len(otherPaths) > 0 {
		paths, e := p.getLocalPaths(p.Parser.GetOtherPaths())
		if e != nil {
			return updated, e
		}
		// append otherPaths to extraPaths
		for _, path := range paths {
			extraPaths[path] = struct{}{}
		}
	}
	// remove local.catalogPath, local.dataPath, and local.depotPath from extraPaths
	delete(extraPaths, vdb.Spec.Local.GetCatalogPath())
	delete(extraPaths, vdb.Spec.Local.DataPath)
	delete(extraPaths, vdb.Spec.Local.DepotPath)

	if len(extraPaths) > 0 {
		paths := maps.Keys(extraPaths)
		sort.Strings(paths)
		extraPathsStr := strings.Join(paths, ",")
		if extraPathsStr != vmeta.GetExtraLocalPaths(vdb.Annotations) {
			vdb.Annotations[vmeta.ExtraLocalPathsAnnotation] = extraPathsStr
			updated = true
		}
	}
	return updated, err
}

// updateLocalPathsInVdb will update the local.catalogPath, local.dataPath, and local.depotPath if needed
func (p *Planner) updateLocalPathsInVdb(vdb *vapi.VerticaDB, extraPaths map[string]struct{}) (updated bool, err error) {
	catPaths, err := p.getLocalPaths(p.Parser.GetCatalogPaths())
	if err != nil {
		return updated, err
	}
	if catPaths[0] != vdb.Spec.Local.GetCatalogPath() {
		p.logPathChange("catalog", vdb.Spec.Local.GetCatalogPath(), catPaths[0])
		vdb.Spec.Local.CatalogPath = catPaths[0]
		updated = true
	}

	dataPaths, err := p.getLocalPaths(p.Parser.GetDataPaths())
	if err != nil {
		return updated, err
	}
	// If data path is not in the list, use the first one in the list to update it.
	// Later, we can improve this by changing local.dataPath to be an array.
	if !slices.Contains(dataPaths, vdb.Spec.Local.DataPath) {
		p.logPathChange("data", vdb.Spec.Local.DataPath, dataPaths[0])
		vdb.Spec.Local.DataPath = dataPaths[0]
		updated = true
	}

	// append dataPaths to extraPaths
	for _, path := range dataPaths {
		extraPaths[path] = struct{}{}
	}

	depotPaths, err := p.getLocalPaths(p.Parser.GetDepotPaths())
	if err != nil {
		return updated, err
	}
	// If depot path is not in the list, use the first one in the list to update it.
	// Later, we can improve this by changing local.depotPath to be an array.
	if !slices.Contains(depotPaths, vdb.Spec.Local.DepotPath) {
		p.logPathChange("depot", vdb.Spec.Local.DepotPath, depotPaths[0])
		vdb.Spec.Local.DepotPath = depotPaths[0]
		updated = true
	}
	// append depotPaths to extraPaths
	for _, path := range depotPaths {
		extraPaths[path] = struct{}{}
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

// getLocalPaths takes a slice of file paths and returns the common prefixes
// of Vertica auto-generated paths, along with user-created paths. It ignores
// non-absolute paths, as these may represent remote storage locations.
func (p *Planner) getLocalPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no paths passed in")
	}
	localPaths := make(map[string]struct{})
	// Store a prefix for Vertica auto-generated paths.
	// This prefix will be put in front of the returned paths.
	verticaPrefix := ""
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			p.Log.Info("Ignored a non-absolute path since it could be a remote storage location", "path", path)
			continue
		}
		if fp, ok := p.extractPathPrefixFromVNodePath(path); ok {
			localPaths[fp] = struct{}{}
			if verticaPrefix == "" {
				verticaPrefix = fp
			}
		} else {
			// For user-specified paths, we don't use prefixes because we want to restrict
			// users to accessing only the directories they originally created in the database.
			// Allowing access to other directories under those prefixes could lead to unintended access.
			cleanPath := filepath.Clean(path)
			localPaths[cleanPath] = struct{}{}
		}
	}

	if len(localPaths) == 0 {
		return nil, fmt.Errorf("no local paths found")
	}
	delete(localPaths, verticaPrefix)
	// convert the map to a slice
	localPathSlice := make([]string, 0, len(localPaths))
	for prefix := range localPaths {
		localPathSlice = append(localPathSlice, prefix)
	}
	// prepend verticaPrefix to localPathSlice
	if verticaPrefix != "" {
		localPathSlice = append([]string{verticaPrefix}, localPathSlice...)
	}
	return localPathSlice, nil
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
