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

package vadmin

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RestartNode will restart a subset of nodes. Use this when vertica has not
// lost cluster quorum. The IP given for each vnode may not match the current IP
// in the vertica catalogs.
func (a *Admintools) RestartNode(ctx context.Context, opts ...restartnode.Option) (ctrl.Result, error) {
	s := restartnode.Parms{}
	s.Make(opts...)
	cmd := a.genRestartNodeCmd(&s)
	stdout, err := a.execAdmintools(ctx, s.InitiatorName, cmd...)
	if err != nil {
		return a.logFailure("restart_node", events.MgmtFailed, stdout, err)
	}
	return ctrl.Result{}, nil
}

// genRestartNodeCmd returns the command to run to restart a pod
func (a *Admintools) genRestartNodeCmd(s *restartnode.Parms) []string {
	hostVNodes := make([]string, 0, len(s.RestartHosts))
	for vnode := range s.RestartHosts {
		hostVNodes = append(hostVNodes, vnode)
	}
	// Sort by vnode so the order of nodes we restart is consistent.
	sort.Slice(hostVNodes, func(i, j int) bool {
		return hostVNodes[i] < hostVNodes[j]
	})
	hostIPs := make([]string, 0, len(s.RestartHosts))
	for _, vnode := range hostVNodes {
		hostIPs = append(hostIPs, s.RestartHosts[vnode])
	}
	cmd := []string{
		"-t", "restart_node",
		"--database=" + a.VDB.Spec.DBName,
		"--hosts=" + strings.Join(hostVNodes, ","),
		"--new-host-ips=" + strings.Join(hostIPs, ","),
		"--noprompt",
	}
	if a.VDB.GetRestartTimeout() != 0 {
		cmd = append(cmd, fmt.Sprintf("--timeout=%d", a.VDB.GetRestartTimeout()))
	}
	return cmd
}
