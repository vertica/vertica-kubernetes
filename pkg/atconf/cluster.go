/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package atconf

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	configparser "github.com/bigkevmcd/go-configparser"
	"github.com/go-logr/logr"
	"github.com/lithammer/dedent"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"k8s.io/apimachinery/pkg/types"
)

// ClusterWriter is a writer for admintools.conf in an actual cluster
type ClusterWriter struct {
	Log            logr.Logger
	PRunner        cmds.PodRunner
	Vdb            *vapi.VerticaDB
	ATConfTempFile string
}

// MakeClusterWriter will build and return the ClusterWriter struct
func MakeClusterWriter(log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner) Writer {
	return &ClusterWriter{
		Log:     log,
		Vdb:     vdb,
		PRunner: prunner,
	}
}

// AddHosts will had ips to an admintools.conf.  New admintools.conf, stored in
// a temporarily, is returned by name.
func (c *ClusterWriter) AddHosts(ctx context.Context, sourcePod types.NamespacedName, ips []string) (string, error) {
	if err := c.createAdmintoolsConfBase(ctx, sourcePod); err != nil {
		return "", err
	}
	if err := c.addHostsToAdmintoolsConf(ips); err != nil {
		return "", err
	}
	return c.ATConfTempFile, nil
}

// SPILLY - need to handle the case where the pod is already part of the list.  We should treat it as a no-op

// createAdmintoolsConfBase will generate within the operator the
// admintools.conf that we will add our newly installed pods too.  This handles
// creating a file from scratch or copying one from the install pod.
func (c *ClusterWriter) createAdmintoolsConfBase(ctx context.Context, sourcePod types.NamespacedName) error {
	tmp, err := ioutil.TempFile("", "admintools.conf.")
	if err != nil {
		return err
	}
	defer tmp.Close()
	c.ATConfTempFile = tmp.Name()

	// If no name given for the source pod then we create a default one from
	// scratch.  Otherwise we read the current admintools.conf from the install
	// pod into the temp file.
	if sourcePod == (types.NamespacedName{}) {
		err = c.writeDefaultAdmintoolsConf(tmp)
		if err != nil {
			return err
		}
	} else {
		// SPILLY - have a shared const for ServerContainer
		stdout, _, err := c.PRunner.ExecInPod(ctx, sourcePod, "server", "cat", paths.AdminToolsConf)
		if err != nil {
			return nil
		}
		_, err = tmp.WriteString(stdout)
		if err != nil {
			return nil
		}
	}
	tmp.Close()

	return nil
}

// addHostsToAdmintoolsConf will add the newly installed hosts to the
// admintools.conf that we are building on the operator.  This depends on
// d.ATConfTempFile being set and the file populated.
func (c *ClusterWriter) addHostsToAdmintoolsConf(installIPs []string) error {
	cp, err := configparser.NewConfigParserFromFile(c.ATConfTempFile)
	if err != nil {
		return err
	}

	err = c.addNewHosts(cp, installIPs)
	if err != nil {
		return err
	}
	err = cp.SaveWithDelimiter(c.ATConfTempFile, "=")
	if err != nil {
		return err
	}
	return nil
}

// addNewHosts adds the pods as new hosts to the admintools.conf file.  It works
// on admintools.conf using the in-memory ConfigParser representation.
func (c *ClusterWriter) addNewHosts(cp *configparser.ConfigParser, installIPs []string) error {
	// SPILLY - this function is long.
	var ips strings.Builder
	existingHosts, err := cp.Get("Cluster", "hosts")
	// Ignore error in case the hosts option doesn't exist
	if err == nil {
		ips.WriteString(existingHosts)
	}
	for _, ip := range installIPs {
		if ips.Len() != 0 {
			ips.WriteString(",")
		}
		ips.WriteString(ip)
	}
	err = cp.Set("Cluster", "hosts", ips.String())
	if err != nil {
		return err
	}

	const NodePrefix = "node"
	var nextNodeNumber = 1
	items, err := cp.Items("Nodes")
	if err == nil {
		for k := range items {
			if strings.HasPrefix(k, NodePrefix) {
				i, e2 := strconv.Atoi(k[len(NodePrefix):])
				if e2 != nil {
					continue
				}
				if i >= nextNodeNumber {
					nextNodeNumber = i + 1
				}
			}
		}
	}

	for _, ip := range installIPs {
		nodeName := fmt.Sprintf("node%04d", nextNodeNumber)
		nextNodeNumber++
		nodeInfo := fmt.Sprintf("%s,%s,%s", ip, c.Vdb.Spec.Local.DataPath, c.Vdb.Spec.Local.DataPath)
		err = cp.Set("Nodes", nodeName, nodeInfo)
		if err != nil {
			return err
		}
	}

	return nil
}

// writeDefaultAdmintoolsConf will write out the default admintools.conf for when nothing exists.
// nolint:lll
func (c *ClusterWriter) writeDefaultAdmintoolsConf(file *os.File) error {
	var DefaultAdmintoolsConf = `
	    [Configuration]
		format = 3
		install_opts = 
		default_base = /home/dbadmin
		controlmode = pt2pt
		controlsubnet = default
		spreadlog = False
		last_port = 5433
		tmp_dir = /tmp
		atdebug = False
		atgui_default_license = False
		unreachable_host_caching = True
		aws_metadata_conn_timeout = 2
		rebalance_shards_timeout = 36000
		database_state_change_poll_timeout = 21600
		wait_for_shutdown_timeout = 3600
		pexpect_verbose_logging = False
		sync_catalog_retries = 2000
		client_connect_timeout_sec = 5.0
		admintools_config_version = 110

		[Cluster]

		[Nodes]

		[SSHConfig]
		ssh_user = 
		ssh_ident = 
		ssh_options = -oConnectTimeout=30 -o TCPKeepAlive=no -o ServerAliveInterval=15 -o ServerAliveCountMax=2 -o StrictHostKeyChecking=no -o BatchMode=yes

		[BootstrapParameters]
		awsendpoint = null
		awsregion = null`

	_, err := file.WriteString(dedent.Dedent(DefaultAdmintoolsConf))
	return err
}
