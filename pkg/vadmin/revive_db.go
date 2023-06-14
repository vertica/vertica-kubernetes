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
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReviveDB will initialize a database from an existing communal path.
// Admintools is used to run the revive.
func (a Admintools) ReviveDB(ctx context.Context, opts ...revivedb.Option) (ctrl.Result, error) {
	s := revivedb.Parms{}
	s.Make(opts...)
	cmd := a.genReviveCmd(&s)
	stdout, _, err := a.PRunner.ExecAdmintools(ctx, s.Initiator, names.ServerContainer, cmd...)
	if err != nil {
		return a.logFailure("revive_db", events.ReviveDBFailed, stdout, err)
	}
	return ctrl.Result{}, nil
}

// DescribeDB will get information about a database from communal storage. For
// the admintools implementation, this is running the revive with the
// --display-only option.
func (a Admintools) DescribeDB(ctx context.Context, opts ...describedb.Option) (string, ctrl.Result, error) {
	s := describedb.Parms{}
	s.Make(opts...)
	cmd := a.genDescribeCmd(&s)
	stdout, _, err := a.PRunner.ExecAdmintools(ctx, s.Initiator, names.ServerContainer, cmd...)
	if err != nil {
		res, err2 := a.logFailure("revive_db", events.ReviveDBFailed, stdout, err)
		return "", res, err2
	}
	return stdout, ctrl.Result{}, nil
}

// ReviveDB will initialized a database using an existing communal path. It does
// this using the vclusterops library.
func (v VClusterOps) ReviveDB(ctx context.Context, opts ...revivedb.Option) (ctrl.Result, error) {
	// SPILLY - log a message here
	return ctrl.Result{}, fmt.Errorf("not implemented %v", v)
}

func (v VClusterOps) DescribeDB(ctx context.Context, opts ...describedb.Option) (string, ctrl.Result, error) {
	return "", ctrl.Result{}, fmt.Errorf("not implemented %v", v)
}

func (v Fake) ReviveDB(ctx context.Context, opts ...revivedb.Option) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (v Fake) DescribeDB(ctx context.Context, opts ...describedb.Option) (string, ctrl.Result, error) {
	return "", ctrl.Result{}, nil
}

// genReviveCmd will generate the command line options for calling admintools -t revive_db
func (a Admintools) genReviveCmd(s *revivedb.Parms) []string {
	cmd := []string{
		"-t", "revive_db",
		"--hosts=" + strings.Join(s.Hosts, ","),
		"--database", s.DBName,
	}

	// If a communal path is set, include all of the EON parameters.
	if s.CommunalPath != "" {
		cmd = append(cmd,
			fmt.Sprintf("--communal-storage-location=%s", s.CommunalPath),
			fmt.Sprintf("--communal-storage-params=%s", paths.AuthParmsFile),
		)
	}

	if s.IgnoreClusterLease {
		cmd = append(cmd, "--ignore-cluster-lease")
	}
	return cmd
}

// genDescribeCmd will generate the command line options for calling
// admintools -t revive_db --display-only.
func (a Admintools) genDescribeCmd(s *describedb.Parms) []string {
	// SPILLY - remove enterprise?
	return []string{
		"-t", "revive_db",
		"--display-only",
		"--database", s.DBName,
		fmt.Sprintf("--communal-storage-location=%s", s.CommunalPath),
		fmt.Sprintf("--communal-storage-params=%s", paths.AuthParmsFile),
	}
}
