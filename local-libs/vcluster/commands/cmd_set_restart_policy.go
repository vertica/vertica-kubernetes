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

/* CmdSetRestartPolicy
 *
 * Implements ClusterCommand interface
 */
type CmdSetRestartPolicy struct {
	restartPolicyOptions *vclusterops.VSetRestartPolicyOptions

	CmdBase
}

func makeCmdSetRestartPolicy() *cobra.Command {
	newCmd := &CmdSetRestartPolicy{}
	opt := vclusterops.VSetRestartPolicyOptionsFactory()
	newCmd.restartPolicyOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		SetRestartPolicy,
		"Set restart policy for a database",
		`Set restart policy for a database, which could be ksafe (default), never, or always.

Examples:
  # Set restart policy to always
  vcluster set_restart_policy --policy always
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, passwordFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdSetRestartPolicy) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.restartPolicyOptions.Policy,
		restartPolicyFlag,
		util.DefaultRestartPolicy,
		"The restart policy of the database.",
	)
}

func (c *CmdSetRestartPolicy) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	return c.validateParse(logger)
}

func (c *CmdSetRestartPolicy) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.restartPolicyOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	return c.ValidateParseBaseOptions(&c.restartPolicyOptions.DatabaseOptions)
}

func (c *CmdSetRestartPolicy) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")

	err := vcc.VSetRestartPolicy(c.restartPolicyOptions)
	if err != nil {
		return err
	}

	vcc.DisplayInfo("Successfully set restart policy as %q", c.restartPolicyOptions.Policy)

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdSetRestartPolicy
func (c *CmdSetRestartPolicy) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.restartPolicyOptions.DatabaseOptions = *opt
}
