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

/* CmdUpgradePart1
 *
 * A subcommand creating a sandbox for upgrading vertica
 *
 * Implements ClusterCommand interface
 */
type CmdUpgradePart1 struct {
	CmdBase
	upgradeVerticaOptions *vclusterops.VUpgradeVerticaOptions
}

func makeCmdUpgradePart1() *cobra.Command {
	newCmd := &CmdUpgradePart1{}
	opt := vclusterops.VUpgradeVerticaOptionsFactory()
	newCmd.upgradeVerticaOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		upgradePart1SubCmd,
		"Begin vertica upgrade.",
		`Begin vertica upgrade by creating a sandbox and redirecting connections to it.

This operation is a prerequisite to the other upgrade_vertica subcommands.

Examples:
# TODO: examples
`,
		[]string{catalogPathFlag, communalStorageLocationFlag, configFlag, dbNameFlag, eonModeFlag, hostsFlag, ipv6Flag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	return cmd
}

func (c *CmdUpgradePart1) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.upgradeVerticaOptions.SandboxName,
		"sandbox-name",
		"upgrade-sb",
		"Name of the sandbox that will be created for online upgrade.",
	)
	cmd.Flags().StringSliceVar(
		&c.upgradeVerticaOptions.SandboxSubclusters,
		"sandbox-subclusters",
		[]string{},
		"Comma separated list of subclusters to be added to the sandbox.",
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
}

func (c *CmdUpgradePart1) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	return nil
}

func (c *CmdUpgradePart1) Run(vcc vclusterops.ClusterCommands) error {
	c.upgradeVerticaOptions.Phase = vclusterops.UpgradeVerticaPhase1
	c.upgradeVerticaOptions.PostSandboxHook = c
	if err := vcc.VUpgradeVertica(c.upgradeVerticaOptions); err != nil {
		return err
	}

	vcc.DisplayInfo("Creating a sandbox for upgrade has completed. Please upgrade the installed vertica on all sandbox nodes then run %s %s",
		upgradeVerticaSubCmd, upgradePart2SubCmd)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance
func (c *CmdUpgradePart1) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.upgradeVerticaOptions.DatabaseOptions = *opt
}

func (c *CmdUpgradePart1) RunPostSandboxHook(subcluster string, vcc vclusterops.ClusterCommands) {
	defer vcc.DisplayInfo("Successfully sandboxed subcluster %s as %s", subcluster, c.upgradeVerticaOptions.SandboxName)
	// Read and then update the sandbox information on config file
	dbConfig, err := readConfig()
	if err != nil {
		vcc.DisplayWarning("Failed to read the configuration file %v; skipping configuration file update", err)
		return
	}
	// Update config
	updatedConfig := false
	for _, n := range dbConfig.Nodes {
		if subcluster == n.Subcluster {
			n.Sandbox = c.upgradeVerticaOptions.SandboxName
			updatedConfig = true
		}
	}
	if !updatedConfig {
		vcc.DisplayWarning("Skipping configuration file update for sandboxed subcluster " + subcluster +
			"; information about subcluster nodes missing from configuration file")
		return
	}

	if err := dbConfig.write(c.upgradeVerticaOptions.ConfigPath, true /*forceOverwrite*/, vcc.GetLog()); err != nil {
		vcc.DisplayWarning(util.FailToWriteToConfig + err.Error())
		return
	}
}
