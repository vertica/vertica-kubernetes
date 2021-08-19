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
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ClusterSection          = "Cluster"
	NodesSection            = "Nodes"
	ConfigurationSection    = "Configuration"
	ClusterHostOption       = "hosts"
	ConfigurationIPv6Option = "ipv6"
)

// FileWriter is a writer for admintools.conf in an actual cluster
type FileWriter struct {
	Log            logr.Logger
	PRunner        cmds.PodRunner
	Vdb            *vapi.VerticaDB
	ATConfTempFile string
	Cfg            *configparser.ConfigParser
}

// MakeFileWriter will build and return the ClusterWriter struct
func MakeFileWriter(log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner) Writer {
	return &FileWriter{
		Log:     log,
		Vdb:     vdb,
		PRunner: prunner,
	}
}

// AddHosts will had ips to an admintools.conf.  New admintools.conf, stored in
// a temporary file, is returned by name.  It is the callers responsibility to
// clean it up.
func (f *FileWriter) AddHosts(ctx context.Context, sourcePod types.NamespacedName, ips []string) (string, error) {
	if err := f.createAdmintoolsConfBase(ctx, sourcePod); err != nil {
		return "", err
	}
	if err := f.loadATConf(); err != nil {
		return "", err
	}
	if err := f.setIPv6Flag(ips); err != nil {
		return "", err
	}
	if err := f.addHostsToAdmintoolsConf(ips); err != nil {
		return "", err
	}
	return f.ATConfTempFile, nil
}

// RemoveHosts will remove IPs from admintools.conf.  New admintools.conf,
// stored in a temporary file, is returned by name to the caller.  The caller is
// responsible for removing this file.
func (f *FileWriter) RemoveHosts(ctx context.Context, sourcePod types.NamespacedName, ips []string) (string, error) {
	if err := f.createAdmintoolsConfBase(ctx, sourcePod); err != nil {
		return "", err
	}
	if err := f.loadATConf(); err != nil {
		return "", err
	}
	if err := f.removeHostsFromAdmintoolsConf(ips); err != nil {
		return "", err
	}
	return f.ATConfTempFile, nil
}

// createAdmintoolsConfBase will generate within the operator the
// admintools.conf that we will add our newly installed pods too.  This handles
// creating a file from scratch or copying one from the install pod.
func (f *FileWriter) createAdmintoolsConfBase(ctx context.Context, sourcePod types.NamespacedName) error {
	tmp, err := ioutil.TempFile("", "admintools.conf.")
	if err != nil {
		return err
	}
	defer tmp.Close()
	f.ATConfTempFile = tmp.Name()

	// If no name given for the source pod then we create a default one from
	// scratch.  Otherwise we read the current admintools.conf from the install
	// pod into the temp file.
	if sourcePod == (types.NamespacedName{}) {
		err = f.writeDefaultAdmintoolsConf(tmp)
		if err != nil {
			return err
		}
	} else {
		stdout, _, err := f.PRunner.ExecInPod(ctx, sourcePod, names.ServerContainer, "cat", paths.AdminToolsConf)
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

// loadATConf will load the admintools.conf file into memory in Cfg
func (f *FileWriter) loadATConf() error {
	var err error
	f.Cfg, err = configparser.NewConfigParserFromFile(f.ATConfTempFile)
	if err != nil {
		return err
	}
	return nil
}

// setIPv6Flag will set the ipv6 flag in the config
func (f *FileWriter) setIPv6Flag(installIPs []string) error {
	if len(installIPs) == 0 {
		return nil
	}
	var flagVal string
	if net.IsIPv6(installIPs[0]) {
		flagVal = "True"
	} else {
		flagVal = "False"
	}
	return f.Cfg.Set(ConfigurationSection, ConfigurationIPv6Option, flagVal)
}

// addHostsToAdmintoolsConf will add the newly installed hosts to the
// admintools.conf that we are building on the operator.  This depends on
// d.ATConfTempFile being set and the file populated.
func (f *FileWriter) addHostsToAdmintoolsConf(installIPs []string) error {
	if err := f.addNewHosts(installIPs); err != nil {
		return err
	}
	return f.Cfg.SaveWithDelimiter(f.ATConfTempFile, "=")
}

// removeHostsFromAdmintoolsConf will remove the IPs from the admintools.conf.
// Changes are made to the parsed f.Cfg struct.
func (f *FileWriter) removeHostsFromAdmintoolsConf(ips []string) error {
	if err := f.removeOldHosts(ips); err != nil {
		return err
	}
	return f.Cfg.SaveWithDelimiter(f.ATConfTempFile, "=")
}

// addNewHosts adds the pods as new hosts to the admintools.conf file.  It works
// on admintools.conf using the in-memory ConfigParser representation.
func (f *FileWriter) addNewHosts(installIPs []string) error {
	oldHosts := f.getHosts()
	if err := f.addToClusterHosts(oldHosts, installIPs); err != nil {
		return err
	}
	return f.addNodes(oldHosts, installIPs)
}

// removeOldHosts will remove the given IPs from the admintools.conf.  Changes
// are made in-place in ConfigParser.
func (f *FileWriter) removeOldHosts(ips []string) error {
	if err := f.removeFromClusterHosts(ips); err != nil {
		return err
	}
	return f.removeNodes(ips)
}

// addToClusterHosts will add the given set of installIPs as new hosts to the
// Cluster section.  The updates are done in-place in the ConfigParser.
func (f *FileWriter) addToClusterHosts(oldHosts map[string]bool, installIPs []string) error {
	var ips strings.Builder
	oldHostLine, err := f.Cfg.Get(ClusterSection, ClusterHostOption)
	// Ignore error in case the hosts option doesn't exist
	if err == nil {
		ips.WriteString(oldHostLine)
	}
	for _, ip := range installIPs {
		// If host already exists, we treat as a no-op and skip the host
		if _, ok := oldHosts[ip]; ok {
			continue
		}
		if ips.Len() != 0 {
			ips.WriteString(",")
		}
		ips.WriteString(ip)
	}
	err = f.Cfg.Set(ClusterSection, ClusterHostOption, ips.String())
	if err != nil {
		return err
	}
	return nil
}

// removeFromClusteHosts will remove a set of IPs from the Cluster.hosts section
// of the config.  Changes are made in-place in the ConfigParser.
func (f *FileWriter) removeFromClusterHosts(ips []string) error {
	// SPILLY - use const
	oldHostLine, err := f.Cfg.Get(ClusterSection, ClusterHostOption)
	if err != nil {
		return err
	}
	hosts := strings.Split(oldHostLine, ",")
	for _, removeIP := range ips {
		for i := len(hosts) - 1; i >= 0; i-- {
			if hosts[i] == removeIP {
				hosts = append(hosts[0:i], hosts[i+1:]...)
				break
			}
		}
	}
	return f.Cfg.Set(ClusterSection, ClusterHostOption, strings.Join(hosts, ","))
}

// addNodes will add the given set of installIPs as new nodes in the Nodes
// section.  The updates are done in-place in the ConfigParser.
func (f *FileWriter) addNodes(oldHosts map[string]bool, installIPs []string) error {
	nextNodeNumber := f.getNextNodeNumber()
	for _, ip := range installIPs {
		// If host already exists, we treat as a no-op and skip the host
		if _, ok := oldHosts[ip]; ok {
			continue
		}
		nodeName := fmt.Sprintf("node%04d", nextNodeNumber)
		nextNodeNumber++
		nodeInfo := fmt.Sprintf("%s,%s,%s", ip, f.Vdb.Spec.Local.DataPath, f.Vdb.Spec.Local.DataPath)
		err := f.Cfg.Set(NodesSection, nodeName, nodeInfo)
		if err != nil {
			return err
		}
	}
	return nil
}

// removeNodes will remove the nodes section for the given set of IPs
func (f *FileWriter) removeNodes(ips []string) error {
	for _, ip := range ips {
		nodes, err := f.Cfg.Items(NodesSection)
		if err != nil {
			return err
		}
		for option, details := range nodes {
			if strings.Contains(details, fmt.Sprintf("%s,", ip)) {
				err = f.Cfg.RemoveOption(NodesSection, option)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// getHosts will build a map of all hosts that currently exist in the config
func (f *FileWriter) getHosts() map[string]bool {
	existingHosts, err := f.Cfg.Get(ClusterSection, ClusterHostOption)
	// Ignore error in case the hosts option doesn't exist
	if err != nil {
		return map[string]bool{}
	}
	lk := map[string]bool{}
	for _, host := range strings.Split(existingHosts, ",") {
		lk[host] = true
	}
	return lk
}

// getNextNodeNumber returns the number to use for the next vertica node.  It
// determines this by parsing the current config.
func (f *FileWriter) getNextNodeNumber() int {
	const NodePrefix = "node"
	var nextNodeNumber = 1
	items, err := f.Cfg.Items(NodesSection)
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
	return nextNodeNumber
}

// writeDefaultAdmintoolsConf will write out the default admintools.conf for when nothing exists.
// nolint:lll
func (f *FileWriter) writeDefaultAdmintoolsConf(file *os.File) error {
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
