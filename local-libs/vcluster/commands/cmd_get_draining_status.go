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
 The subcommand (get_draining_status) within this file is intended solely for internal testing purposes.
 It is not designed, intended, or authorized for use in production environments. The behavior of this
 subcommand may change without prior notice and is not guaranteed to be maintained in future releases.
*/

package commands

import (
	"encoding/json"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdGetDrainingStatus
 *
 * Implements ClusterCommand interface
 */
type CmdGetDrainingStatus struct {
	CmdBase
	getDrainingStatusOpt *vclusterops.VGetDrainingStatusOptions
}

func makeCmdGetDrainingStatus() *cobra.Command {
	// CmdGetDrainingStatus
	newCmd := &CmdGetDrainingStatus{}
	opt := vclusterops.VGetDrainingStatusFactory()
	newCmd.getDrainingStatusOpt = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		getDrainingStatusSubCmd,
		"Get draining status.",
		`Get draining status.

Examples:
  # Show draining status of all subclusters in main cluster with user input
  vcluster get_draining_status --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --password "PASSWORD"

  # Show draining status of all subclusters in main cluster with config file
  vcluster get_draining_status \
    --config /opt/vertica/config/vertica_cluster.yaml \
    --password "PASSWORD"

  # Show draining status of all subclusters in a sandbox with config file
  vcluster get_draining_status --sandbox sand1 \
    --config /opt/vertica/config/vertica_cluster.yaml \
    --password "PASSWORD"
`,
		[]string{dbNameFlag, configFlag, passwordFlag, hostsFlag, ipv6Flag, sandboxFlag,
			eonModeFlag, outputFileFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// hide this subcommand
	cmd.Hidden = true

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdGetDrainingStatus) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.getDrainingStatusOpt.Sandbox,
		sandboxFlag,
		"",
		"The name of target sandbox",
	)
}

func (c *CmdGetDrainingStatus) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.getDrainingStatusOpt.DatabaseOptions)

	// get_draining_status only works for an Eon db so we assume the user always runs this subcommand
	// on an Eon db. When Eon mode cannot be found in config file, we set its value to true.
	if !viper.IsSet(eonModeKey) {
		c.getDrainingStatusOpt.IsEon = true
	}

	return c.validateParse(logger)
}

func (c *CmdGetDrainingStatus) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.getDrainingStatusOpt.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.getDrainingStatusOpt.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.setDBPassword(&c.getDrainingStatusOpt.DatabaseOptions)
	if err != nil {
		return err
	}
	return nil
}

func (c *CmdGetDrainingStatus) Analyze(logger vlog.Printer) error {
	logger.Info("Called method Analyze()")
	return nil
}

func (c *CmdGetDrainingStatus) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")

	options := c.getDrainingStatusOpt

	drainingStatusList, err := vcc.VGetDrainingStatus(options)
	if err != nil {
		vcc.LogError(err, "failed to get draining status list", "cluster", util.GetClusterName(options.Sandbox))
		return err
	}
	bytes, err := json.MarshalIndent(drainingStatusList, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	c.writeCmdOutputToFile(globals.file, bytes, vcc.GetLog())

	vcc.DisplayInfo("Successfully displayed draining status list for %s", util.GetClusterName(options.Sandbox))
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdGetDrainingStatus
func (c *CmdGetDrainingStatus) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.getDrainingStatusOpt.DatabaseOptions = *opt
}
