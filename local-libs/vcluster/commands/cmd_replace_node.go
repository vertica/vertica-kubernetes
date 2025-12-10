/*
 (c) Copyright [2023-2025] Open Text.
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
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdReplaceNode
 *
 * Implements ClusterCommand interface
 */
type CmdReplaceNode struct {
	replaceNodeOptions *vclusterops.VReplaceNodeOptions
	CmdBase
}

func makeCmdReplaceNode() *cobra.Command {
	newCmd := &CmdReplaceNode{}
	opt := vclusterops.VReplaceNodeOptionsFactory()
	newCmd.replaceNodeOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		replaceNodeSubCmd,
		"Replace a database node",
		`Replace a database node.

Examples:
  # Replace a database node
  vcluster replace_node --original-host 10.20.30.43 \
    --new-host 10.20.30.44

  # Replace a database node with database config file
  vcluster replace_node --original-host 10.20.30.43 \
    --new-host 10.20.30.44 \
    --config /opt/vertica/config/vertica_cluster.yaml

  # Replace a database node in a sandbox
  vcluster replace_node --original-host 10.20.30.43 \
    --new-host 10.20.30.44 \
    --sandbox sand
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, dataPathFlag, depotPathFlag,
			passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// Original and replacement hosts are required
	markFlagsRequired(cmd, "original-host", "new-host")

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdReplaceNode) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.replaceNodeOptions.OriginalHost,
		"original-host",
		"",
		"The original node to be replaced",
	)
	cmd.Flags().StringVar(
		&c.replaceNodeOptions.NewHost,
		"new-host",
		"",
		"The host that will replace the old host",
	)
	cmd.Flags().StringVar(
		&c.replaceNodeOptions.Sandbox,
		sandboxFlag,
		"",
		"The name of the sandbox. Required if the old host is in a sandbox.",
	)
	cmd.Flags().IntVar(
		&c.replaceNodeOptions.TimeOut,
		"timeout",
		util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", util.DefaultTimeoutSeconds),
		"The time, in seconds, to wait for the replaced node to start.",
	)
}

func (c *CmdReplaceNode) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.replaceNodeOptions.DatabaseOptions)
	return c.validateParse(logger)
}

func (c *CmdReplaceNode) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	err := c.ValidateParseBaseOptions(&c.replaceNodeOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.replaceNodeOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	return c.setDBPassword(&c.replaceNodeOptions.DatabaseOptions)
}

func (c *CmdReplaceNode) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")

	options := c.replaceNodeOptions

	// Read sandbox information on config file
	// Hint: This would only work when there is atleast one node in the target subcluster
	dbConfig, configErr := readConfig()
	if configErr != nil {
		vcc.DisplayWarning("Failed to read the configuration file, skipping configuration file update", "error", configErr)
	}

	vdb, err := vcc.VReplaceNode(options)
	if err != nil {
		vcc.LogError(err, "failed to replace node")
		return err
	}

	mainCluster := false
	if c.replaceNodeOptions.Sandbox == util.MainClusterSandbox {
		mainCluster = true
	}
	// write db info to vcluster config file
	if configErr == nil {
		// update new node info in config
		UpdateDBConfig(&vdb, dbConfig, c.replaceNodeOptions.Sandbox, mainCluster)
		writeErr := dbConfig.write(c.replaceNodeOptions.DatabaseOptions.ConfigPath, true /*forceOverwrite*/, vcc.GetLog())
		if writeErr != nil {
			vcc.PrintWarning("Fail to update config file: %s", writeErr)
			return nil
		}
	} else {
		err = writeConfig(&vdb, true /*forceOverwrite*/, vcc.GetLog())
		if err != nil {
			vcc.DisplayWarning("Failed to write config file: %s", err)
		}
	}

	vcc.DisplayInfo("Successfully replaced node")
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdReplaceNode
func (c *CmdReplaceNode) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.replaceNodeOptions.DatabaseOptions = *opt
}
