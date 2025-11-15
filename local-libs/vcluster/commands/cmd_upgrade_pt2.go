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

/* CmdUpgradePart2
 *
 * A subcommand creating a sandbox for upgrading vertica
 *
 * Implements ClusterCommand interface
 */
type CmdUpgradePart2 struct {
	CmdBase
	upgradeVerticaOptions *vclusterops.VUpgradeVerticaOptions
}

func makeCmdUpgradePart2() *cobra.Command {
	newCmd := &CmdUpgradePart2{}
	opt := vclusterops.VUpgradeVerticaOptionsFactory()
	newCmd.upgradeVerticaOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		upgradePart2SubCmd,
		"Continue Vertica Upgrade.",
		`Continue vertica upgrade by replicating changes from the main to the sandbox and redirecting connections to the sandbox.

This operation is a required to be preceded by the create_sandbox upgrade_vertica subcommand,
and is a prerequisite of the finalize subcommand.

Examples:
# TODO: examples`,
		[]string{catalogPathFlag, communalStorageLocationFlag, configFlag, dbNameFlag, eonModeFlag, hostsFlag, ipv6Flag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	return cmd
}

func (c *CmdUpgradePart2) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.upgradeVerticaOptions.SandboxName,
		"sandbox-name",
		"upgrade-sb",
		"Name of the sandbox that will be created for online upgrade.",
	)
	cmd.Flags().StringVar(
		&c.upgradeVerticaOptions.RedirectHosts,
		"redirect-hosts",
		"",
		"Comma separated list of hostnames/ip addresses to redirect sandbox-subcluster connections to.",
	)
	cmd.Flags().IntVar(
		&c.upgradeVerticaOptions.PollingTimeout,
		"timeout",
		util.DefaultTimeoutSeconds,
		"The timeout (in seconds) to poll for sandbox status and for connections to be redirected.",
	)
	cmd.Flags().StringVar(
		&c.upgradeVerticaOptions.ReplicateTLSConfig,
		sourceTLSConfigFlag,
		"",
		"The TLS configuration to use when replicating from the main to sandbox cluster.",
	)
}

func (c *CmdUpgradePart2) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	return nil
}

func (c *CmdUpgradePart2) Run(vcc vclusterops.ClusterCommands) error {
	c.upgradeVerticaOptions.Phase = vclusterops.UpgradeVerticaPhase2
	if err := vcc.VUpgradeVertica(c.upgradeVerticaOptions); err != nil {
		return err
	}

	vcc.DisplayInfo("Replicating data and redirecting connections from old to new cluster complete. "+
		"Please upgrade the installed vertica on all remaining nodes then run %s %s", upgradeVerticaSubCmd, upgradePart3SubCmd)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance
func (c *CmdUpgradePart2) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.upgradeVerticaOptions.DatabaseOptions = *opt
}
