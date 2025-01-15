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
	"github.com/spf13/viper"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdAddSubcluster
 *
 * Parses arguments to addSubcluster and calls
 * the high-level function for addSubcluster.
 *
 * Implements ClusterCommand interface
 */

type CmdAddSubcluster struct {
	CmdBase
	addSubclusterOptions *vclusterops.VAddSubclusterOptions
	scHostListStr        string
}

func makeCmdAddSubcluster() *cobra.Command {
	// CmdAddSubcluster
	newCmd := &CmdAddSubcluster{}
	opt := vclusterops.VAddSubclusterOptionsFactory()
	newCmd.addSubclusterOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		addSCSubCmd,
		"Add a subcluster",
		`This command adds a new subcluster to an Eon Mode database.

Examples:
  # Add a subcluster with config file
  vcluster add_subcluster --subcluster sc1 \
    --config /opt/vertica/config/vertica_cluster.yaml \
    --is-primary --control-set-size 1 \
    --password "PASSWORD"

  # Add a subcluster with user input
  vcluster add_subcluster --subcluster sc1 --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --is-primary --control-set-size -1 \
    --password "PASSWORD"

  # Add a subcluster and new nodes in the subcluster with config file
  vcluster add_subcluster --subcluster sc1 \
    --config /opt/vertica/config/vertica_cluster.yaml \
    --is-primary --control-set-size 1 --new-hosts 10.20.30.43 \
    --password "PASSWORD"

  # Add a subcluster new nodes in the subcluster with user input
  vcluster add_subcluster --subcluster sc1 --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --is-primary --control-set-size -1 --new-hosts 10.20.30.43 \
    --password "PASSWORD"
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, eonModeFlag, passwordFlag,
			dataPathFlag, depotPathFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// check if hidden flags can be implemented/removed in VER-92259
	// hidden flags
	newCmd.setHiddenFlags(cmd)

	// require name of subcluster to add
	markFlagsRequired(cmd, subclusterFlag)

	// hide eon mode flag since we expect it to come from config file, not from user input
	hideLocalFlags(cmd, []string{eonModeFlag})

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdAddSubcluster) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.addSubclusterOptions.SCName,
		subclusterFlag,
		"",
		"The name of the new subcluster. This string must conform to the format used for database names.",
	)
	cmd.Flags().BoolVar(
		&c.addSubclusterOptions.IsPrimary,
		"is-primary",
		false,
		"Whether the new subcluster should be a primary subcluster. If this option is omitted, new subclusters are secondary.",
	)
	cmd.Flags().IntVar(
		&c.addSubclusterOptions.ControlSetSize,
		"control-set-size",
		vclusterops.ControlSetSizeDefaultValue,
		"The number of control nodes in the subcluster (default: -1, all nodes in the subcluster are control nodes).",
	)
	cmd.Flags().StringSliceVar(
		&c.addSubclusterOptions.NewHosts,
		addNodeFlag,
		[]string{},
		"A comma-separated list of hosts or IP addresses to add to the subcluster.",
	)
	cmd.Flags().BoolVar(
		&c.addSubclusterOptions.ForceRemoval,
		"force-removal",
		false,
		"Whether to delete any existing database directories in the new hosts before attempting to add them.",
	)
	cmd.Flags().BoolVar(
		c.addSubclusterOptions.SkipRebalanceShards,
		"skip-rebalance-shards",
		false,
		util.GetEonFlagMsg("Whether to skip shard rebalancing."),
	)
	cmd.Flags().StringVar(
		&c.addSubclusterOptions.DepotSize,
		"depot-size",
		"",
		util.GetEonFlagMsg(util.DepotFmtMsg+util.DepotSizeKMGTMsg+
			"integer%, which expresses the depot size as a percentage of the total disk size.\n"),
	)
}

// setHiddenFlags will set the hidden flags the command has.
// These hidden flags will not be shown in help and usage of the command, and they will be used internally.
func (c *CmdAddSubcluster) setHiddenFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.scHostListStr,
		"sc-hosts",
		"",
		"",
	)
	cmd.Flags().StringVar(
		&c.addSubclusterOptions.CloneSC,
		"like",
		"",
		"",
	)
	hideLocalFlags(cmd, []string{"sc-hosts", "like"})
}

func (c *CmdAddSubcluster) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// reset some options that are not included in user input
	c.ResetUserInputOptions(&c.addSubclusterOptions.DatabaseOptions)

	// add_subcluster only works for an Eon db so we assume the user always runs this subcommand
	// on an Eon db. When Eon mode cannot be found in config file, we set its value to true.
	if !viper.IsSet(eonModeKey) {
		c.addSubclusterOptions.IsEon = true
	}
	return c.validateParse(logger)
}

// all validations of the arguments should go in here
func (c *CmdAddSubcluster) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.addSubclusterOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.addSubclusterOptions.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.addSubclusterOptions.DatabaseOptions)
}

func (c *CmdAddSubcluster) Analyze(logger vlog.Printer) error {
	logger.Info("Called method Analyze()")
	return nil
}

func (c *CmdAddSubcluster) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")

	options := c.addSubclusterOptions

	err := vcc.VAddSubcluster(options)
	if err != nil {
		vcc.LogError(err, "fail to add subcluster")
		return err
	}

	if len(options.NewHosts) > 0 {
		vlog.DisplayColorInfo("Adding hosts %v to subcluster %s", options.NewHosts, options.SCName)

		options.VAddNodeOptions.DatabaseOptions = c.addSubclusterOptions.DatabaseOptions
		options.VAddNodeOptions.SCName = c.addSubclusterOptions.SCName

		vdb, err := vcc.VAddNode(&options.VAddNodeOptions)
		if err != nil {
			const msg = "Failed to add nodes to the new subcluster"
			vcc.DisplayError("%s\nHint: The subcluster %q was successfully created; use add_node to add the nodes.\n",
				msg, options.VAddNodeOptions.SCName)
			return err
		}
		// update db info in the config file
		err = writeConfig(&vdb, true /*forceOverwrite*/)
		if err != nil {
			vcc.DisplayWarning("Failed to write config file, details: %s", err)
		}
	}

	if len(options.NewHosts) > 0 {
		vcc.DisplayInfo("Successfully added subcluster %s with nodes %v to database %s",
			options.SCName, options.NewHosts, options.DBName)
	} else {
		vcc.DisplayInfo("Successfully added subcluster %s to database %s", options.SCName, options.DBName)
	}
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdAddSubcluster
func (c *CmdAddSubcluster) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.addSubclusterOptions.DatabaseOptions = *opt
}
