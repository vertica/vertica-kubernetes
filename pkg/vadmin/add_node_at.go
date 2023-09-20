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
	"strings"

	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addnode"
	corev1 "k8s.io/api/core/v1"
)

// AddNode will add a new vertica node to the cluster. If add node fails due to
// a license limit, the error will be of type addnode.LicenseLimitError.
func (a *Admintools) AddNode(ctx context.Context, opts ...addnode.Option) error {
	s := addnode.Parms{}
	s.Make(opts...)

	// Cleanup for any prior failed attempt.
	for _, pod := range s.PodNames {
		err := a.prepLocalData(ctx, a.VDB, a.PRunner, pod)
		if err != nil {
			return err
		}
	}

	cmd := a.genAddNodeCommand(&s)
	stdout, err := a.execAdmintools(ctx, s.InitiatorName, cmd...)
	if err != nil {
		switch {
		case isLicenseLimitError(stdout):
			a.EVWriter.Event(a.VDB, corev1.EventTypeWarning, events.AddNodeLicenseFail,
				"You cannot add more nodes to the database.  You have reached the limit allowed by your license.")
			// Remap the error to this type so the caller can do a type check to
			// know it was a license limit error.
			err = &addnode.LicenseLimitError{
				Msg: stdout,
			}
		default:
			a.EVWriter.Eventf(a.VDB, corev1.EventTypeWarning, events.AddNodeFailed,
				"Failed when calling 'admintools -t db_add_node' for pod(s) '%s'", strings.Join(s.Hosts, ","))
		}
	}

	return err
}

// isLicenseLimitError returns true if the stdout contains the error about not enough licenses
func isLicenseLimitError(stdout string) bool {
	return strings.Contains(stdout, "Cannot create another node. The current license permits")
}

// genAddNodeCommand returns the command to run to add nodes to the cluster.
func (a *Admintools) genAddNodeCommand(s *addnode.Parms) []string {
	return []string{
		"-t", "db_add_node",
		"--hosts", strings.Join(s.Hosts, ","),
		"--database", a.VDB.Spec.DBName,
		"--subcluster", s.Subcluster,
		"--noprompt",
	}
}
