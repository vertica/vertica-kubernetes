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

package vadmin

import (
	"context"
	"fmt"
	"strings"

	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type DBCreator struct {
	Admintools *Admintools
	Parms      createdb.Parms
}

// CreateDB will create a brand new database using the admintools API (-t create_db).
func (a *Admintools) CreateDB(ctx context.Context, opts ...createdb.Option) (ctrl.Result, error) {
	s := createdb.Parms{}
	s.Make(opts...)
	dbc := DBCreator{
		Admintools: a,
		Parms:      s,
	}

	return a.initDB(ctx, &dbc)
}

// genCreateDBCmd will generate the command line options for calling admintools -t create_db
func (a *Admintools) genCreateDBCmd(s *createdb.Parms) []string {
	cmd := []string{
		"-t", "create_db",
		"--skip-fs-checks",
		"--hosts=" + strings.Join(s.Hosts, ","),
		"--catalog_path=" + s.CatalogPath,
		"--database", s.DBName,
		"--force-removal-at-creation",
		"--noprompt",
		"--license", s.LicensePath,
		"--depot-path=" + s.DepotPath,
	}

	if s.PostDBCreateSQLFile != "" {
		cmd = append(cmd,
			"--sql="+s.PostDBCreateSQLFile,
		)
	}

	// If a communal path is set, include all of the EON parameters.
	if s.CommunalPath != "" {
		cmd = append(cmd,
			fmt.Sprintf("--communal-storage-location=%s", s.CommunalPath),
			fmt.Sprintf("--communal-storage-params=%s", paths.AuthParmsFile),
		)
	}

	if s.ShardCount > 0 {
		cmd = append(cmd,
			fmt.Sprintf("--shard-count=%d", s.ShardCount),
		)
	}

	if s.SkipPackageInstall {
		cmd = append(cmd, "--skip-package-install")
	}

	return cmd
}

// GenCmd will return the command line options for calling admintools -t create_db.
func (d *DBCreator) GenCmd() []string {
	return d.Admintools.genCreateDBCmd(&d.Parms)
}

// GetInitiator returns the initiator pod name.
func (d *DBCreator) GetInitiator() types.NamespacedName {
	return d.Parms.Initiator
}

// GetPodNames returns the pod name list
func (d *DBCreator) GetPodNames() []types.NamespacedName {
	return d.Parms.PodNames
}

// LogFailure will log and record an event for an admintools -t create_db failure
func (d *DBCreator) LogFailure(stdout string, err error) (ctrl.Result, error) {
	return d.Admintools.logFailure("create_db", events.CreateDBFailed, stdout, err)
}

// GetConfigParms returns the configuration parameters map.
func (d *DBCreator) GetConfigParms() map[string]string {
	return d.Parms.ConfigurationParams
}
