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
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdSandbox
 *
 * Implements ClusterCommand interface
 *
 * Parses CLI arguments for sandbox operation.
 * Prepares the inputs for the library.
 *
 */

type CmdSandboxSubcluster struct {
	CmdBase
	sbOptions      vclusterops.VSandboxOptions
	pollingOptions vclusterops.VPollSubclusterStateOptions
}

func (c *CmdSandboxSubcluster) TypeName() string {
	return "CmdSandboxSubcluster"
}

func makeCmdSandboxSubcluster() *cobra.Command {
	// CmdSandboxSubcluster
	newCmd := &CmdSandboxSubcluster{}
	opt := vclusterops.VSandboxOptionsFactory()
	newCmd.sbOptions = opt

	cmd := makeBasicCobraCmd(
		newCmd,
		sandboxSubCmd,
		"Sandboxes a subcluster.",
		`Sandboxes a secondary subcluster in an Eon Mode database.

All hosts in the subcluster must be up.

When you sandbox a subcluster, its hosts immediately shut down and restart;
the subcluster becomes sandboxed after the hosts start back up.

A sandbox can contain multiple subclusters, and subclusters in the sandbox can
interact with each other. If you want to isolate subclusters, they must
be in separate sandboxes.

Subcluster sandboxing should be used for testing changes or upgrades in safe, isolated
environment and should not be used for production subclusters. For example, you can
create sandboxes and then upgrade Vertica in those sandboxes.

Examples:
  # Sandbox a subcluster with config file
  vcluster sandbox_subcluster --subcluster sc1 --sandbox sand \
    --config /opt/vertica/config/vertica_cluster.yaml
    --password "PASSWORD"

  # Sandbox a subcluster with user input
  vcluster sandbox_subcluster --subcluster sc1 --sandbox sand \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 --db-name test_db \
    --password "PASSWORD"
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require name of subcluster to sandbox as well as the sandbox name
	markFlagsRequired(cmd, subclusterFlag, sandboxFlag)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdSandboxSubcluster) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.sbOptions.SCName,
		subclusterFlag,
		"",
		"The name of the subcluster to sandbox.",
	)
	cmd.Flags().StringVar(
		&c.sbOptions.SandboxName,
		sandboxFlag,
		"",
		"The name of the sandbox.",
	)
	cmd.Flags().BoolVar(
		&c.sbOptions.SaveRp,
		saveRpFlag,
		false,
		"Save a restore point when creating the sandbox.",
	)
	cmd.Flags().BoolVar(
		&c.sbOptions.Imeta,
		isolateMetadataFlag,
		false,
		"Isolate the metadata of the sandboxed subcluster.",
	)
	cmd.Flags().BoolVar(
		&c.sbOptions.Sls,
		createStorageLocationsFlag,
		false,
		"The sandbox can create its own storage locations.",
	)
	cmd.Flags().BoolVar(
		&c.sbOptions.ForUpgrade,
		forUpgradeFlag,
		false,
		"The sandbox is to be used for online upgrade",
	)
	cmd.Flags().IntVar(
		&c.pollingOptions.Timeout,
		"timeout",
		util.DefaultTimeoutSeconds,
		"The timeout (in seconds) to poll for sandbox status.",
	)
}

func (c *CmdSandboxSubcluster) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	return c.parseInternal(logger)
}

func (c *CmdSandboxSubcluster) parseInternal(logger vlog.Printer) error {
	logger.Info("Called parseInternal()")

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.sbOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.sbOptions.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.sbOptions.DatabaseOptions)
}

func (c *CmdSandboxSubcluster) Analyze(logger vlog.Printer) error {
	logger.Info("Called method Analyze()")
	return nil
}

func (c *CmdSandboxSubcluster) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo(util.CallCommand + sandboxSubCmd)

	options := c.sbOptions

	err := vcc.VSandbox(&options)
	if err != nil {
		vcc.LogError(err, "failed to sandbox the subcluster.")
		return err
	}

	defer vcc.DisplayInfo("Successfully sandboxed subcluster " + c.sbOptions.SCName + " as " + c.sbOptions.SandboxName)
	// Read and then update the sandbox information on config file
	dbConfig, configErr := readConfig()
	if configErr != nil {
		vcc.DisplayWarning("Failed to read the configuration file, skipping configuration file update", "error", configErr)
		return nil
	}
	// Update config
	updatedConfig := c.updateSandboxInfo(dbConfig)
	if !updatedConfig {
		vcc.DisplayWarning("Did not update node info for sandboxed subcluster " + c.sbOptions.SCName +
			", information about subcluster nodes missing from configuration file, skipping configuration file update")
		return nil
	}

	writeErr := dbConfig.write(options.ConfigPath, true /*forceOverwrite*/)
	if writeErr != nil {
		vcc.DisplayWarning(util.FailToWriteToConfig + writeErr.Error())
		return nil
	}

	options.DatabaseOptions.Hosts = options.SCHosts
	pollOpts := c.pollingOptions
	pollOpts.DatabaseOptions = options.DatabaseOptions
	pollOpts.SkipOptionsValidation = true
	pollOpts.SCName = options.SCName

	err = vcc.VPollSubclusterState(&pollOpts)
	if err != nil {
		vcc.LogError(err, "Failed to wait for sandboxed subcluster to come up")
		return err
	}
	return nil
}

// updateSandboxInfo will update sandbox info for the sandboxed subcluster in the config object
// returns true if the info are updated, returns false if no info is updated
func (c *CmdSandboxSubcluster) updateSandboxInfo(dbConfig *DatabaseConfig) bool {
	needToUpdate := false
	for _, n := range dbConfig.Nodes {
		if c.sbOptions.SCName == n.Subcluster {
			n.Sandbox = c.sbOptions.SandboxName
			needToUpdate = true
		}
	}
	return needToUpdate
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdSandboxSubcluster
func (c *CmdSandboxSubcluster) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.sbOptions.DatabaseOptions = *opt
}
