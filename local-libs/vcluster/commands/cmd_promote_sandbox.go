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

 DISCLAIMER:
 The subcommand (promote_sandbox) within this file is intended solely for internal testing purposes.
 It is not designed, intended, or authorized for use in production environments. The behavior of this
 subcommand may change without prior notice and is not guaranteed to be maintained in future releases.

 Use of this function in any production code or reliance upon its behavior is strongly discouraged and
 undertaken at your own risk. Open Text assumes no responsibility for any consequences arising from the
 use of this function outside of its intended testing context.
*/

package commands

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* cmdPromoteSandbox
 *
 * Parses arguments to promote a sandbox to main cluster.
 * This should not be used by VCluster CLI users. See the disclaimer above.
 *
 * Implements ClusterCommand interface
 */
type cmdPromoteSandbox struct {
	promoteSandboxOpts *vclusterops.VPromoteSandboxToMainOptions
	CmdBase
}

func makeCmdPromoteSandbox() *cobra.Command {
	newCmd := &cmdPromoteSandbox{}
	opt := vclusterops.VPromoteSandboxToMainFactory()
	newCmd.promoteSandboxOpts = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		promoteSandboxSubCmd,
		"",
		"",
		[]string{dbNameFlag, hostsFlag, ipv6Flag, eonModeFlag, configFlag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// required flags
	markFlagsRequired(cmd, sandboxFlag)

	// hide this subcommand
	cmd.Hidden = true

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *cmdPromoteSandbox) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.promoteSandboxOpts.SandboxName,
		sandboxFlag,
		"",
		"The name of the sandbox",
	)
}

func (c *cmdPromoteSandbox) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	// reset some options that are not included in user input
	c.ResetUserInputOptions(&c.promoteSandboxOpts.DatabaseOptions)

	// promote_sandbox only works for an Eon db so we assume the user always runs this subcommand
	// on an Eon db. When Eon mode cannot be found in config file, we set its value to true.
	if !viper.IsSet(eonModeKey) {
		c.promoteSandboxOpts.IsEon = true
	}

	return c.validateParse(logger)
}

func (c *cmdPromoteSandbox) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.promoteSandboxOpts.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.promoteSandboxOpts.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.promoteSandboxOpts.DatabaseOptions)
}

func (c *cmdPromoteSandbox) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.promoteSandboxOpts

	err := vcc.VPromoteSandboxToMain(options)
	if err != nil {
		vcc.LogError(err, "fail to promote sandbox to main", "sandbox", c.promoteSandboxOpts.SandboxName)
		return err
	}
	vcc.DisplayInfo("Successfully promoted sandbox %q to main", c.promoteSandboxOpts.SandboxName)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdPromoteSandbox
func (c *cmdPromoteSandbox) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.promoteSandboxOpts.DatabaseOptions = *opt
}
