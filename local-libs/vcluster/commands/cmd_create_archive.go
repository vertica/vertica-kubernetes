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

/* CmdCreateArchive
 *
 * Parses arguments to create-archive and calls
 * the high-level function for create-archive.
 *
 * Implements ClusterCommand interface
 */

type CmdCreateArchive struct {
	CmdBase
	createArchiveOptions *vclusterops.VCreateArchiveOptions
}

func makeCmdCreateArchive() *cobra.Command {
	newCmd := &CmdCreateArchive{}
	opt := vclusterops.VCreateArchiveFactory()
	newCmd.createArchiveOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		createArchiveCmd,
		"Create an archive in a given archive name and number.",
		`Create an archive in a given archive name and number.

Examples:
  # Create an archive in a given archive name
  vcluster create_archive --db-name DBNAME --archive-name ARCHIVE_ONE --password "PASSWORD"

  # Create an archive in a given archive name and number of restore point(default 3)
  vcluster create_archive --db-name DBNAME --archive-name ARCHIVE_ONE \
    --num-restore-points 6 --password "PASSWORD"

  # Create an archive in main cluster with user input password
  vcluster create_archive --db-name DBNAME --archive-name ARCHIVE_ONE \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 --password "PASSWORD"

  # Create an archive for a sandbox
  vcluster create_archive --db-name DBNAME --archive-name ARCHIVE_ONE \
    --sandbox SANDBOX_ONE --password "PASSWORD"

`,
		[]string{dbNameFlag, configFlag, passwordFlag,
			hostsFlag, ipv6Flag, eonModeFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require archive-name
	markFlagsRequired(cmd, archiveNameFlag)

	// hide this subcommand
	cmd.Hidden = true

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdCreateArchive) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.createArchiveOptions.ArchiveName,
		archiveNameFlag,
		"",
		"The name of archive to be created.",
	)
	cmd.Flags().IntVar(
		&c.createArchiveOptions.NumRestorePoint,
		"num-restore-points",
		vclusterops.CreateArchiveDefaultNumRestore,
		"Maximum number of restore points that archive can contain."+
			"If you provide 0, the number of restore points will be unlimited. "+
			"By default, the value is 0. Negative number is disallowed.",
	)
	cmd.Flags().StringVar(
		&c.createArchiveOptions.Sandbox,
		sandboxFlag,
		"",
		"The name of target sandbox",
	)
}

func (c *CmdCreateArchive) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.createArchiveOptions.DatabaseOptions)

	// create_archive only works for an Eon db so we assume the user always runs this subcommand
	// on an Eon db. When Eon mode cannot be found in config file, we set its value to true.
	if !viper.IsSet(eonModeKey) {
		c.createArchiveOptions.IsEon = true
	}

	return c.validateParse(logger)
}

// all validations of the arguments should go in here
func (c *CmdCreateArchive) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	err := c.ValidateParseBaseOptions(&c.createArchiveOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.setConfigParam(&c.createArchiveOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	if !c.usePassword() {
		err = c.getCertFilesFromCertPaths(&c.createArchiveOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err = c.setDBPassword(&c.createArchiveOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	return nil
}

func (c *CmdCreateArchive) Analyze(logger vlog.Printer) error {
	logger.Info("Called method Analyze()")
	return nil
}

func (c *CmdCreateArchive) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.createArchiveOptions

	err := vcc.VCreateArchive(options)
	if err != nil {
		vcc.LogError(err, "failed to create archive", "archiveName", options.ArchiveName)
		return err
	}

	vcc.DisplayInfo("Successfully created archive: %s", options.ArchiveName)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdCreateArchive
func (c *CmdCreateArchive) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.createArchiveOptions.DatabaseOptions = *opt
}
