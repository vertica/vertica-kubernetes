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

/* CmdUpgradePart3
 *
 * A subcommand creating a sandbox for upgrading vertica
 *
 * Implements ClusterCommand interface
 */
type CmdUpgradePart3 struct {
	CmdBase
	upgradeVerticaOptions *vclusterops.VUpgradeVerticaOptions
}

func makeCmdUpgradePart3() *cobra.Command {
	newCmd := &CmdUpgradePart3{}
	opt := vclusterops.VUpgradeVerticaOptionsFactory()
	newCmd.upgradeVerticaOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		upgradePart3SubCmd,
		"Finalize Vertica Upgrade.",
		`Finish vertica upgrade by starting cluster and reestablishing cluster topology.

This operation is a required to be preceded by first create_sandbox then promote_sandbox upgrade_vertica subcommands.

Examples:
# TODO: examples`,
		[]string{catalogPathFlag, communalStorageLocationFlag, configFlag, dbNameFlag, eonModeFlag, hostsFlag, ipv6Flag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	return cmd
}

func (c *CmdUpgradePart3) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(
		&c.upgradeVerticaOptions.PrimarySubclusters,
		"primary-subclusters",
		[]string{},
		"Comma separated list of primary subclusters",
	)
	cmd.Flags().IntVar(
		&c.upgradeVerticaOptions.PollingTimeout,
		"timeout",
		util.DefaultTimeoutSeconds,
		"The timeout (in seconds) to poll for sandbox status and for connections to be redirected.",
	)
}

func (c *CmdUpgradePart3) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	return nil
}

func (c *CmdUpgradePart3) Run(vcc vclusterops.ClusterCommands) error {
	c.upgradeVerticaOptions.Phase = vclusterops.UpgradeVerticaPhase3
	if err := vcc.VUpgradeVertica(c.upgradeVerticaOptions); err != nil {
		return err
	}

	vcc.DisplayInfo("Vertica upgrade complete!")
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance
func (c *CmdUpgradePart3) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.upgradeVerticaOptions.DatabaseOptions = *opt
}
