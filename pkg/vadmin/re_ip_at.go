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
	"regexp"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// The name of the IP map file that is used by re_ip.  re_ip is only ever used if the entire cluster is down.
	AdminToolsMapFile = "/opt/vertica/config/ipMap.txt"
)

// A map that does a lookup of a vertica node name to an IP address
type verticaIPLookup map[string]string

// ReIP will update the catalog on disk with new IPs for all of the nodes given.
func (a *Admintools) ReIP(ctx context.Context, opts ...reip.Option) (ctrl.Result, error) {
	s := reip.Parms{}
	s.Make(opts...)

	if len(s.Hosts) == 0 {
		return ctrl.Result{}, fmt.Errorf("you must specify at least one host for re-ip")
	}

	// We always use the compat21 nodes when generating the IP map.  We cannot
	// use the vnode because they are only set _after_ a node is added to a DB.
	// ReIP can be dealing with a mix -- some nodes that have been added to the
	// db and some that aren't.
	oldIPs, err := a.fetchOldIPsFromNode(ctx, s.Initiator)
	if err != nil {
		return ctrl.Result{}, err
	}

	mapFileContents, ipChanging := a.genMapFile(oldIPs, &s)
	if !ipChanging {
		// no re-ip is necessary, the IP are not changing
		return ctrl.Result{}, nil
	}

	cmd := a.genMapFileUploadCmd(mapFileContents)
	if _, _, err = a.PRunner.ExecInPod(ctx, s.Initiator, names.ServerContainer, cmd...); err != nil {
		return ctrl.Result{}, err
	}

	cmd, err = a.genReIPCommand()
	if err != nil {
		return ctrl.Result{}, err
	}
	if _, err = a.execAdmintools(ctx, s.Initiator, cmd...); err != nil {
		// Log an event as failure to re_ip means we won't be able to bring up the database.
		a.EVWriter.Event(a.VDB, corev1.EventTypeWarning, events.ReipFailed,
			"Attempt to run 'admintools -t re_ip' failed")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// genMapFile generates the map file used by re_ip
// The list of old IPs are passed in. We combine that with the new IPs in the
// podfacts to generate the map file. The map file is returned as a list of
// strings. Its format is what is expected by admintools -t re_ip.
func (a *Admintools) genMapFile(oldIPs verticaIPLookup, s *reip.Parms) (mapContents []string, ipChanging bool) {
	mapContents = []string{}
	ipChanging = false

	for _, host := range s.Hosts {
		nodeName := host.Compat21Node
		oldIP, ok := oldIPs[nodeName]
		// If we are missing the old IP, we skip and don't fail.  Re-ip allows
		// for a subset of the nodes and the host may already be removed from
		// the cluster anyway.
		if !ok {
			continue
		}
		if oldIP != host.IP {
			ipChanging = true
		}
		mapContents = append(mapContents, fmt.Sprintf("%s %s", oldIP, host.IP))
	}
	return mapContents, ipChanging
}

// genReIPCommand will return the command to run for the re_ip command
func (a *Admintools) genReIPCommand() ([]string, error) {
	cmd := []string{
		"-t", "re_ip",
		"--file=" + AdminToolsMapFile,
		"--noprompt",
	}

	// In 11.1, we added a --force option to re_ip to allow us to run it while
	// some nodes are up.  This was done to support doing a reip while there are
	// read-only secondary nodes.
	vinf, err := a.VDB.MakeVersionInfoCheck()
	if err != nil {
		return nil, err
	}
	if vinf.IsEqualOrNewer(vapi.ReIPAllowedWithUpNodesVersion) {
		cmd = append(cmd, "--force")
	}

	return cmd, nil
}

// genMapFileUploadCmd returns the command to run to upload the map file
func (a *Admintools) genMapFileUploadCmd(mapFileContents []string) []string {
	return []string{
		"bash", "-c", "cat > " + AdminToolsMapFile + "<<< '" + strings.Join(mapFileContents, "\n") + "'",
	}
}

// fetchOldIPsFromNode will read a local admintools.conf and get the IPs from it.
// The IPs from an admintools.conf represent the *old* IPs. We store them in a
// map, where the lookup is by the node name. This function only handles
// compat21 node names.
func (a *Admintools) fetchOldIPsFromNode(ctx context.Context, atPod types.NamespacedName) (verticaIPLookup, error) {
	cmd := a.genGrepNodeCmd()
	stdout, _, err := a.PRunner.ExecInPod(ctx, atPod, names.ServerContainer, cmd...)
	if err != nil {
		return verticaIPLookup{}, err
	}
	return parseNodesFromAdmintoolConf(stdout), nil
}

// genGrepNodeCmd returns the command to run to get the nodes from admintools.conf
// This function only handles grepping compat21 nodes.
func (a *Admintools) genGrepNodeCmd() []string {
	return []string{
		"bash", "-c", fmt.Sprintf("grep --regexp='^node[0-9]' %s", paths.AdminToolsConf),
	}
}

// parseNodesFromAdmintoolConf will parse out the vertica node and IP from admintools.conf output.
// The nodeText passed in is taken from a grep output of the node columns. As
// such, multiple lines are concatenated together with '\n'.
func parseNodesFromAdmintoolConf(nodeText string) verticaIPLookup {
	ips := make(verticaIPLookup)
	rs := `^(node\d{4}) = ([\d.:a-fA-F]+),`

	re := regexp.MustCompile(rs)
	for _, line := range strings.Split(nodeText, "\n") {
		match := re.FindAllStringSubmatch(line, 1)
		if len(match) > 0 && len(match[0]) >= 3 {
			ips[match[0][1]] = match[0][2]
		}
	}
	return ips
}
