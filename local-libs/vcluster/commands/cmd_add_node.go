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
	"strings"

	"github.com/spf13/cobra"

	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdAddNode
 *
 * Implements ClusterCommand interface
 */
type CmdAddNode struct {
	addNodeOptions *vclusterops.VAddNodeOptions
	// Comma-separated list of node names, which exist in the cluster
	nodeNameListStr string

	CmdBase
}

func makeCmdAddNode() *cobra.Command {
	// CmdAddNode
	newCmd := &CmdAddNode{}
	opt := vclusterops.VAddNodeOptionsFactory()
	newCmd.addNodeOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		addNodeSubCmd,
		"Adds host(s) to a database",
		`Adds one or more user-specified hosts as nodes to an existing database. You cannot add nodes to a sandboxed subcluster.

Examples:
  # Add a single host to the existing database with config file
  vcluster add_node --db-name test_db --new-hosts 10.20.30.43 \
    --config /opt/vertica/config/vertica_cluster.yaml \
    --password "PASSWORD"

  # Add multiple hosts to the existing database with user input
  vcluster add_node --db-name test_db --new-hosts 10.20.30.43,10.20.30.44 \
    --data-path /data --hosts 10.20.30.40 \
    --node-names v_test_db_node0001,v_test_db_node0002 \
    --password "PASSWORD"
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, dataPathFlag, depotPathFlag,
			passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require hosts to add
	markFlagsRequired(cmd, addNodeFlag)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdAddNode) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(
		&c.addNodeOptions.NewHosts,
		addNodeFlag,
		[]string{},
		"A comma-separated list of hosts to add to the database.",
	)
	cmd.Flags().BoolVar(
		&c.addNodeOptions.ForceRemoval,
		"force-removal",
		false,
		"Whether to delete any existing database directories in the new hosts before attempting to add them.",
	)
	cmd.Flags().BoolVar(
		c.addNodeOptions.SkipRebalanceShards,
		"skip-rebalance-shards",
		false,
		util.GetEonFlagMsg("Whether to skip shard rebalancing."),
	)
	cmd.Flags().StringVar(
		&c.addNodeOptions.SCName,
		subclusterFlag,
		"",
		util.GetEonFlagMsg("The name of the subcluster to which the host(s) should be added."+
			"This string must conform to the format used for database names."),
	)
	cmd.Flags().StringVar(
		&c.addNodeOptions.DepotSize,
		"depot-size",
		"",
		util.GetEonFlagMsg(util.DepotFmtMsg+util.DepotSizeKMGTMsg+util.DepotSizeHint),
	)
	cmd.Flags().StringVar(
		&c.nodeNameListStr,
		"node-names",
		"",
		"[Use only with support guidance] A comma-separated list of node names that exist in the cluster.",
	)
	cmd.Flags().StringVar(
		&c.addNodeOptions.ComputeGroup,
		"compute-group",
		"",
		util.GetEonFlagMsg("The new or existing compute group for the new nodes. "+
			"If specified, the new nodes will be compute-only nodes."),
	)
	cmd.Flags().IntVar(
		&c.addNodeOptions.TimeOut,
		"add-node-timeout",
		util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", util.DefaultTimeoutSeconds),
		"The time, in seconds, to wait for the specified nodes to be added.",
	)
}

func (c *CmdAddNode) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.addNodeOptions.DatabaseOptions)
	return c.validateParse(logger)
}

func (c *CmdAddNode) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	err := c.parseNewHostList()
	if err != nil {
		return err
	}

	err = c.parseNodeNameList()
	if err != nil {
		return err
	}

	err = c.ValidateParseBaseOptions(&c.addNodeOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.addNodeOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	return c.setDBPassword(&c.addNodeOptions.DatabaseOptions)
}

// parseNewHostList trims and lowercases the hosts in --add
func (c *CmdAddNode) parseNewHostList() error {
	if len(c.addNodeOptions.NewHosts) > 0 {
		err := util.ParseHostList(&c.addNodeOptions.NewHosts)
		if err != nil {
			// the err from util.ParseHostList will be "must specify a host or host list"
			// we overwrite the error here to provide more details
			return fmt.Errorf("you must specify at least one host")
		}
	}
	return nil
}

func (c *CmdAddNode) parseNodeNameList() error {
	// if --node-names is set, there must be at least one node name
	if c.parser.Changed("node-names") {
		if c.nodeNameListStr == "" {
			return fmt.Errorf("when --node-names is specified, "+
				"you must provide all existing nodes in %q", c.addNodeOptions.DBName)
		}

		c.addNodeOptions.ExpectedNodeNames = strings.Split(c.nodeNameListStr, ",")
	}

	return nil
}

func (c *CmdAddNode) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")

	options := c.addNodeOptions

	vdb, err := vcc.VAddNode(options)
	if err != nil {
		vcc.LogError(err, "failed to add node")
		return err
	}

	// write db info to vcluster config file
	err = writeConfig(&vdb, true /*forceOverwrite*/)
	if err != nil {
		vcc.DisplayWarning("Failed to write config file: %s", err)
	}

	vcc.DisplayInfo("Successfully added nodes %v to database %s", c.addNodeOptions.NewHosts, options.DBName)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdAddNode
func (c *CmdAddNode) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.addNodeOptions.DatabaseOptions = *opt
}
