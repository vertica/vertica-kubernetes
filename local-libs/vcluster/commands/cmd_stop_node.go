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
	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdStopNode
 * Implements ClusterCommand interface
 */

type CmdStopNode struct {
	stopNodeOptions *vclusterops.VStopNodeOptions
	CmdBase
}

func makeCmdStopNode() *cobra.Command {
	newCmd := &CmdStopNode{}
	opt := vclusterops.VStopNodeOptionsFactory()
	newCmd.stopNodeOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		stopNodeCmd,
		"Stops one or more nodes in a database.",
		`Stops one or more nodes in a database.

You must provide the host list with the --stop-hosts option followed 
by one or more hosts to stop as a comma-separated list.

Caution: If you only have just enough nodes up to establish database quorum 
and you stop a node, you will lose database quorum and the remaining up 
nodes will be set to read-only mode to prevent data loss.

Examples:
  # Gracefully stop a node with config file
  vcluster stop_node --stop-hosts 10.20.30.43 \
    --config /home/dbadmin/vertica_cluster.yaml \
    --password "PASSWORD"

  # Gracefully stop nodes with user input
  vcluster stop_node --db-name test_db --stop-hosts 10.20.30.40,10.20.30.41 \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --password "PASSWORD"
`,
		[]string{dbNameFlag, hostsFlag, ipv6Flag, configFlag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require hosts to stop
	markFlagsRequired(cmd, stopNodeFlag)
	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdStopNode) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(
		&c.stopNodeOptions.StopHosts,
		stopNodeFlag,
		[]string{},
		"Comma-separated list of host(s) to stop",
	)
}

func (c *CmdStopNode) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	// reset some options that are not included in user input
	c.ResetUserInputOptions(&c.stopNodeOptions.DatabaseOptions)
	return c.validateParse(logger)
}

func (c *CmdStopNode) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.stopNodeOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.stopNodeOptions.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.stopNodeOptions.DatabaseOptions)
}

func (c *CmdStopNode) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.stopNodeOptions

	err := vcc.VStopNode(options)
	if err != nil {
		vcc.LogError(err, "failed to stop the nodes", "Nodes", c.stopNodeOptions.StopHosts)
		return err
	}
	vcc.DisplayInfo("Successfully stopped the nodes %v", c.stopNodeOptions.StopHosts)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdStopNode
func (c *CmdStopNode) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.stopNodeOptions.DatabaseOptions = *opt
}
