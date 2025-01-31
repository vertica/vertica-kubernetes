/*
 (c) Copyright [2023-2024] Open Text.
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

package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdStartNodes
 *
 * Implements ClusterCommand interface
 */
type CmdStartNodes struct {
	CmdBase
	startNodesOptions *vclusterops.VStartNodesOptions

	// comma-separated list of vnode=host
	vnodeHostMap map[string]string

	// comma-separated list of hosts
	rawStartHostList []string
}

func makeCmdStartNodes() *cobra.Command {
	// CmdStartNodes
	newCmd := &CmdStartNodes{}
	opt := vclusterops.VStartNodesOptionsFactory()
	newCmd.startNodesOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		startNodeSubCmd,
		"Starts nodes in a running cluster",
		`Starts nodes in a running cluster. This differs from start_db, which starts Vertica after cluster quorum is lost.

One of --restart and --start-hosts is required.

Examples:
  # Start a single node in the database with config file
  vcluster start_node --db-name test_db \
    --start v_test_db_node0004=10.20.30.43 --password "PASSWORD" \
    --config /opt/vertica/config/vertica_cluster.yaml

  # Start a single node and change its IP address in the database
  # with config file (assuming the node IP address previously stored
  # catalog was not 10.20.30.44)
  vcluster start_node --db-name test_db \
    --start v_test_db_node0004=10.20.30.44 --password "PASSWORD" \
    --config /opt/vertica/config/vertica_cluster.yaml

  # Start multiple nodes in the database with config file
  vcluster start_node --db-name test_db \
    --start v_test_db_node0003=10.20.30.42,v_test_db_node0004=10.20.30.43 \
    --password "PASSWORD" --config /opt/vertica/config/vertica_cluster.yaml	
`,
		[]string{dbNameFlag, hostsFlag, ipv6Flag, configFlag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require nodes or hosts to start
	markFlagsOneRequired(cmd, []string{startNodeFlag, startHostFlag})

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdStartNodes) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringToStringVar(
		&c.vnodeHostMap,
		startNodeFlag,
		map[string]string{},
		"A comma-separated list of node_name=ip_address pairs, specifying the nodes to restart.\n"+
			"If ip_address doesn't match the database's listed IP address for that node, Vertica updates\n"+
			"its catalog information for that node with the specified IP address and then restarts the node.",
	)
	cmd.Flags().StringSliceVar(
		&c.rawStartHostList,
		startHostFlag,
		[]string{},
		"A comma-separated list of hosts to start.",
	)
	cmd.Flags().IntVar(
		&c.startNodesOptions.StatePollingTimeout,
		"timeout",
		util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", util.DefaultTimeoutSeconds),
		"The timeout (in seconds) to wait for polling node state operation",
	)

	// users only input --start or --start-hosts
	cmd.MarkFlagsMutuallyExclusive([]string{startNodeFlag, startHostFlag}...)
}

func (c *CmdStartNodes) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.startNodesOptions.DatabaseOptions)

	return c.validateParse(logger)
}

func (c *CmdStartNodes) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	// the node-host map can be loaded from the value of
	// either --start or --start-hosts
	if len(c.rawStartHostList) > 0 {
		err := c.buildStartNodeHostMap()
		if err != nil {
			return err
		}
	} else {
		err := c.startNodesOptions.ParseNodesList(c.vnodeHostMap)
		if err != nil {
			return err
		}
	}

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.startNodesOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.startNodesOptions.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.startNodesOptions.DatabaseOptions)
}

func (c *CmdStartNodes) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")

	options := c.startNodesOptions

	// this is the instruction that will be used by both CLI and operator
	err := vcc.VStartNodes(options)
	if err != nil {
		vcc.LogError(err, "failed to start node.")
		return err
	}

	// all nodes unreachable, nothing need to be done.
	if len(options.Nodes) == 0 {
		vcc.DisplayInfo("No reachable nodes to start")
		return nil
	}

	var hostToStart []string
	for _, ip := range options.Nodes {
		hostToStart = append(hostToStart, ip)
	}
	vcc.DisplayInfo("Successfully started hosts %s of the database %s", hostToStart, options.DBName)

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdStartNodes
func (c *CmdStartNodes) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.startNodesOptions.DatabaseOptions = *opt
}

func (c *CmdStartNodes) buildStartNodeHostMap() error {
	dbConfig, err := readConfig()
	if err != nil {
		return fmt.Errorf("--start-hosts can only be used when "+
			"the configuration file is available: %w", err)
	}

	hostNodeMap := make(map[string]string)
	for _, n := range dbConfig.Nodes {
		hostNodeMap[n.Address] = n.Name
	}

	for _, rawHost := range c.rawStartHostList {
		ip, err := util.ResolveToOneIP(rawHost, c.startNodesOptions.IPv6)
		if err != nil {
			return err
		}
		nodeName, ok := hostNodeMap[ip]
		if !ok {
			return fmt.Errorf("cannot find the address %s (of host %s) from the config file",
				ip, rawHost)
		}
		c.startNodesOptions.Nodes[nodeName] = ip
	}

	return nil
}
