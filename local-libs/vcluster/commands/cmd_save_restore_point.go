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

/* CmdSaveRestorePoint
 *
 * Parses arguments to save-restore-points and calls
 * the high-level function for save-restore-points.
 *
 * Implements ClusterCommand interface
 */

type CmdSaveRestorePoint struct {
	CmdBase
	saveRestoreOptions *vclusterops.VSaveRestorePointOptions
}

func makeCmdSaveRestorePoint() *cobra.Command {
	newCmd := &CmdSaveRestorePoint{}
	opt := vclusterops.VSaveRestorePointFactory()
	newCmd.saveRestoreOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		saveRestorePointsSubCmd,
		"Save a restore point in a given archive.",
		`Save a restore point in a given archive.

Examples:
  # Save restore point in a given archive with user input
  vcluster save_restore_point --db-name test_db \
	--archive-name ARCHIVE_ONE \
	--password "PASSWORD"

  # Save restore point for a sandbox
  vcluster save_restore_point --db-name test_db \
	--archive-name ARCHIVE_ONE --sandbox SANDBOX_ONE \
	--password "PASSWORD"

`,
		[]string{dbNameFlag, hostsFlag, passwordFlag,
			ipv6Flag, configFlag, eonModeFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require db-name and archive-name
	markFlagsRequired(cmd, dbNameFlag, archiveNameFlag)

	// hide this subcommand
	cmd.Hidden = true

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdSaveRestorePoint) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.saveRestoreOptions.ArchiveName,
		archiveNameFlag,
		"",
		"Collection of restore points that belong to a certain archive.",
	)
	cmd.Flags().StringVar(
		&c.saveRestoreOptions.Sandbox,
		sandboxFlag,
		"",
		"The name of target sandbox",
	)
}

func (c *CmdSaveRestorePoint) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.saveRestoreOptions.DatabaseOptions)

	// save_restore_point only works for an Eon db so we assume the user always runs this subcommand
	// on an Eon db. When Eon mode cannot be found in config file, we set its value to true.
	if !viper.IsSet(eonModeKey) {
		c.saveRestoreOptions.IsEon = true
	}

	return c.validateParse(logger)
}

// all validations of the arguments should go in here
func (c *CmdSaveRestorePoint) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	err := c.ValidateParseBaseOptions(&c.saveRestoreOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	if !c.usePassword() {
		err = c.getCertFilesFromCertPaths(&c.saveRestoreOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err = c.setConfigParam(&c.saveRestoreOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.setDBPassword(&c.saveRestoreOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	return nil
}

func (c *CmdSaveRestorePoint) Analyze(logger vlog.Printer) error {
	logger.Info("Called method Analyze()")
	return nil
}

func (c *CmdSaveRestorePoint) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.saveRestoreOptions

	err := vcc.VSaveRestorePoint(options)
	if err != nil {
		vcc.LogError(err, "failed to save restore points", "DBName", options.DBName)
		return err
	}

	vcc.DisplayInfo("Successfully saved restore points in database %s", options.DBName)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdSaveRestorePoint
func (c *CmdSaveRestorePoint) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.saveRestoreOptions.DatabaseOptions = *opt
}
