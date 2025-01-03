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
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdRemoveSubcluster
 *
 * Implements ClusterCommand interface
 */
type CmdRemoveSubcluster struct {
	removeScOptions *vclusterops.VRemoveScOptions

	CmdBase
}

func makeCmdRemoveSubcluster() *cobra.Command {
	// CmdRemoveSubcluster
	newCmd := &CmdRemoveSubcluster{}
	opt := vclusterops.VRemoveScOptionsFactory()
	newCmd.removeScOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		removeSCSubCmd,
		"Removes a subcluster.",
		`Removes a non-sandboxed subcluster and its nodes from an Eon Mode database.

Examples:
  # Remove a subcluster with config file
  vcluster remove_subcluster --subcluster sc1 \
    --config /opt/vertica/config/vertica_cluster.yaml \
    --password "PASSWORD"

  # Remove a subcluster with user input
  vcluster remove_subcluster --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 --subcluster sc1 \
    --data-path /data --depot-path /data \
    --password "PASSWORD"
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, eonModeFlag, dataPathFlag, depotPathFlag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require name of subcluster to remove
	markFlagsRequired(cmd, subclusterFlag)

	// hide eon mode flag since we expect it to come from config file, not from user input
	hideLocalFlags(cmd, []string{eonModeFlag})

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdRemoveSubcluster) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.removeScOptions.SCName,
		subclusterFlag,
		"",
		"Name of subcluster to remove.",
	)
}

func (c *CmdRemoveSubcluster) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// reset some options that are not included in user input
	c.ResetUserInputOptions(&c.removeScOptions.DatabaseOptions)

	// remove_subcluster only works for an Eon db so we assume the user always runs this subcommand
	// on an Eon db. When Eon mode cannot be found in config file, we set its value to true.
	if !viper.IsSet(eonModeKey) {
		c.removeScOptions.IsEon = true
	}
	return c.validateParse(logger)
}

func (c *CmdRemoveSubcluster) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.removeScOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.removeScOptions.DatabaseOptions)
	if err != nil {
		return nil
	}
	return c.setDBPassword(&c.removeScOptions.DatabaseOptions)
}

func (c *CmdRemoveSubcluster) Analyze(_ vlog.Printer) error {
	return nil
}

func (c *CmdRemoveSubcluster) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")

	options := c.removeScOptions

	vdb, err := vcc.VRemoveSubcluster(options)
	if err != nil {
		vcc.LogError(err, "failed to remove subcluster.")
		return err
	}

	vcc.DisplayInfo("Successfully removed subcluster %s from database %s",
		options.SCName, options.DBName)

	// write db info to vcluster config file
	err = writeConfig(&vdb, true /*forceOverwrite*/)
	if err != nil {
		vcc.DisplayWarning("Failed to write the configuration file: %s", err)
	}

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdRemoveSubcluster
func (c *CmdRemoveSubcluster) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.removeScOptions.DatabaseOptions = *opt
}
