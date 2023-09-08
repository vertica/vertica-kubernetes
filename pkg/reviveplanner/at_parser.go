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
	"bufio"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

// The format of the next few struct's is implemented in the server. Do not add
// new fields in here, unless those new fields are also exposed by the server's
// 'admintools -t revive_db --display-only' output.

// CommunalLocation shows information about the database's communal storage
type CommunalLocation struct {
	CommunalStorageURL string `json:"communal_storage_url"`
	NumShards          string `json:"num_shards"`
	DepotPath          string `json:"depot_path"`
	DepotSize          string `json:"depot_size"`
}

// Database is a top-level database struct
type Database struct {
	Nodes                 []Node `json:"nodes"`
	Name                  string `json:"name"`
	Version               int    `json:"version"`
	SpreadVersion         int    `json:"spreadversion"`
	ControlMode           string `json:"controlmode"`
	WillUgrade            bool   `json:"willupgrade"`
	SpreadEncryption      string `json:"spreadEncryption"`
	SpreadEncryptionInUse bool   `json:"spreadEncryptionInUse"`
}

// Node shows information about a single node
type Node struct {
	Name                    string            `json:"name"`
	OID                     int64             `json:"oid"`
	CatalogPath             string            `json:"catalogpath"`
	StorageLocs             []string          `json:"storagelocs"`
	VStorageLocations       []StorageLocation `json:"_vstorage_locations"`
	CommunalStorageLocation StorageLocation   `json:"_communal_storage_location"`
	Host                    string            `json:"host"`
	Port                    int               `json:"port"`
	ControlNode             int64             `json:"controlnode"`
	Deps                    [][]string        `json:"deps"`
	StartCmd                string            `json:"startcmd"`
	IsPrimary               bool              `json:"isprimary"`
}

// StorageLocation has details about a single storage location
type StorageLocation struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	OID         int64  `json:"oid"`
	Path        string `json:"path"`
	SharingType int    `json:"sharing_type"`
	Usage       int    `json:"usage"`
	Size        int    `json:"size"`
	Site        int64  `json:"site"`
}

// MakeATParserFromVDB will make a cluster config parser based on the passed in vdb.
// This is used for tests only.
func MakeATParserFromVDB(vdb *vapi.VerticaDB, logger logr.Logger) ClusterConfigParser {
	db := Database{
		Name: vdb.Spec.DBName,
	}
	nc := 1
	for i := range vdb.Spec.Subclusters {
		for j := 0; j < int(vdb.Spec.Subclusters[i].Size); j++ {
			db.Nodes = append(db.Nodes, Node{
				Name:        fmt.Sprintf("v_%s_node%04d", db.Name, nc),
				CatalogPath: fmt.Sprintf("%s/v_%s_node%04d_catalog", vdb.GetDBCatalogPath(), db.Name, nc),
				IsPrimary:   vdb.Spec.Subclusters[i].IsPrimary,
				VStorageLocations: []StorageLocation{
					{
						Path:  fmt.Sprintf("%s/%s/v_%s_node%04d_data", vdb.Spec.Local.DataPath, db.Name, db.Name, nc),
						Usage: UsageIsDataTemp,
					}, {
						Path:  fmt.Sprintf("%s/%s/v_%s_node%04d_depot", vdb.Spec.Local.DepotPath, db.Name, db.Name, nc),
						Usage: UsageIsDepot,
					},
				},
			})
			nc++
		}
	}
	communalLocation := CommunalLocation{
		NumShards: fmt.Sprintf("%d", vdb.Spec.ShardCount),
	}
	return &ATParser{
		Log:              logger,
		Database:         db,
		CommunalLocation: communalLocation,
		ParseComplete:    true, // We mimiced parse by filling in Database and communal info
	}
}

// GetDataPaths returns the data paths for the node
func (n *Node) GetDataPaths() []string {
	return n.getPathsByUsage(UsageIsDataTemp)
}

// GetDepotPath returns the depot paths for the node
func (n *Node) GetDepotPath() []string {
	return n.getPathsByUsage(UsageIsDepot)
}

// getPathsByUsage returns the path for a given usage type. See Usage* const at
// the top of this file.
func (n *Node) getPathsByUsage(usage int) []string {
	paths := []string{}
	for i := range n.VStorageLocations {
		if n.VStorageLocations[i].Usage == usage {
			paths = append(paths, n.VStorageLocations[i].Path)
		}
	}
	return paths
}

// Parse looks at the op string passed in and spits out Database and
// CommunalLocation structs.
func (a *ATParser) Parse(op string) error {
	// We only parse once. No-op if parse already done.
	if a.ParseComplete {
		return nil
	}
	var rawJSON string
	rawJSON = a.extractCommunalLocation(op)
	if err := json.Unmarshal([]byte(rawJSON), &a.CommunalLocation); err != nil {
		return err
	}

	rawJSON = a.extractDatabase(op)
	return json.Unmarshal([]byte(rawJSON), &a.Database)
}

// extractDatabase parses the full output and returns the json portion that
// pertains to the database.
func (a *ATParser) extractDatabase(op string) string {
	startingMarker := regexp.MustCompile(`^\s*== Database and node details: ==`)
	endingMarker := regexp.MustCompile(`^\s*== `)
	return a.extractGeneric(op, startingMarker, endingMarker)
}

// extractCommunalLocation parses the full output and returns only the portion
// that is for the communal location information.
func (a *ATParser) extractCommunalLocation(op string) string {
	startingMarker := regexp.MustCompile(`^\s*== Communal location details: ==`)
	endingMarker := regexp.MustCompile(`^\s*Cluster lease expiration:`)
	return a.extractGeneric(op, startingMarker, endingMarker)
}

// extractGeneric is a general parsing function of the revive_db --display-only output.
func (a *ATParser) extractGeneric(op string, startingMarker, endingMarker *regexp.Regexp) string {
	scanner := bufio.NewScanner(strings.NewReader(op))
	var sb strings.Builder
	for scanner.Scan() {
		if startingMarker.MatchString(scanner.Text()) {
			for scanner.Scan() {
				if endingMarker.MatchString(scanner.Text()) {
					break
				}
				sb.WriteString(scanner.Text())
			}
			break
		}
	}
	return sb.String()
}
