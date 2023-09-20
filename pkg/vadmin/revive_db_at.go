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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type DBReviver struct {
	Admintools *Admintools
	Parms      revivedb.Parms
}

// ReviveDB will initialize a database from an existing communal path.
// Admintools is used to run the revive.
func (a *Admintools) ReviveDB(ctx context.Context, opts ...revivedb.Option) (ctrl.Result, error) {
	s := revivedb.Parms{}
	s.Make(opts...)
	dbr := DBReviver{
		Admintools: a,
		Parms:      s,
	}

	return a.initDB(ctx, &dbr)
}

// genReviveCmd will generate the command line options for calling admintools -t revive_db
func (a *Admintools) genReviveCmd(s *revivedb.Parms) []string {
	cmd := []string{
		"-t", "revive_db",
		"--hosts=" + strings.Join(s.Hosts, ","),
		"--database", s.DBName,
		fmt.Sprintf("--communal-storage-location=%s", s.CommunalPath),
		fmt.Sprintf("--communal-storage-params=%s", paths.AuthParmsFile),
	}

	if s.IgnoreClusterLease {
		cmd = append(cmd, "--ignore-cluster-lease")
	}
	return cmd
}

// GenCmd will return the command line options for calling admintools -t revive_db.
func (d *DBReviver) GenCmd() []string {
	return d.Admintools.genReviveCmd(&d.Parms)
}

// GetInitiator returns the initiator pod name.
func (d *DBReviver) GetInitiator() types.NamespacedName {
	return d.Parms.Initiator
}

// GetPodNames returns the pod name list
func (d *DBReviver) GetPodNames() []types.NamespacedName {
	return d.Parms.PodNames
}

// LogFailure will log and record an event for an admintools -t revive_db failure
func (d *DBReviver) LogFailure(stdout string, err error) (ctrl.Result, error) {
	return d.Admintools.logFailure("revive_db", events.ReviveDBFailed, stdout, err)
}

// GetConfigParms returns the configuration parameters map.
func (d *DBReviver) GetConfigParms() map[string]string {
	return d.Parms.ConfigurationParams
}
