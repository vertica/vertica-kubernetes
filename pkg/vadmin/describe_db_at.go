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

	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DescribeDB will get information about a database from communal storage. For
// the admintools implementation, this is running the revive with the
// --display-only option.
func (a *Admintools) DescribeDB(ctx context.Context, opts ...describedb.Option) (string, ctrl.Result, error) {
	s := describedb.Parms{}
	s.Make(opts...)
	if err := a.copyAuthFile(ctx, s.Initiator, a.genAuthParmsFileContent(s.ConfigurationParams)); err != nil {
		return "", ctrl.Result{}, err
	}
	cmd := a.genDescribeCmd(&s)
	stdout, err := a.execAdmintools(ctx, s.Initiator, cmd...)
	if err != nil {
		res, err2 := a.logFailure("revive_db", events.ReviveDBFailed, stdout, err)
		return "", res, err2
	}
	return stdout, ctrl.Result{}, nil
}

// genDescribeCmd will generate the command line options for calling
// admintools -t revive_db --display-only.
func (a *Admintools) genDescribeCmd(s *describedb.Parms) []string {
	return []string{
		"-t", "revive_db",
		"--display-only",
		"--database", s.DBName,
		fmt.Sprintf("--communal-storage-location=%s", s.CommunalPath),
		fmt.Sprintf("--communal-storage-params=%s", paths.AuthParmsFile),
	}
}
