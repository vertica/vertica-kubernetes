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
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

const (
	dotYaml = ".yaml"
	dotYml  = ".yml"
)

/* CmdCreateConnection
 *
 * Implements ClusterCommand interface
 */
type CmdCreateConnection struct {
	connectionOptions *vclusterops.VReplicationDatabaseOptions
	CmdBase
}

func makeCmdCreateConnection() *cobra.Command {
	newCmd := &CmdCreateConnection{}
	opt := vclusterops.VReplicationDatabaseFactory()
	newCmd.connectionOptions = &opt
	opt.TargetDB.Password = new(string)

	cmd := makeBasicCobraCmd(
		newCmd,
		createConnectionSubCmd,
		"Creates a file with connection information for the target database.",
		`Creates a file with connection information for the target database. 
The generated connection file should be used with the replication command.

Examples:
  # create the connection file to /tmp/vertica_connection.yaml
  vcluster create_connection --db-name platform_test_db --hosts 10.20.30.43 --db-user \ 
    dkr_dbadmin --password-file /tmp/password.txt --conn /tmp/vertica_connection.yaml
`,
		[]string{connFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	markFlagsRequired(cmd, dbNameFlag, hostsFlag, connFlag)
	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdCreateConnection) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.connectionOptions.TargetDB.DBName,
		dbNameFlag,
		"",
		"The name of the database. You should only use this option if you want to override the database name in your configuration file.",
	)
	cmd.Flags().StringSliceVar(
		&c.connectionOptions.TargetDB.Hosts,
		hostsFlag,
		[]string{},
		"A comma-separated list of hosts in database.")
	cmd.Flags().StringVar(
		&c.connectionOptions.TargetDB.UserName,
		dbUserFlag,
		"",
		"The name of the user in the target database.",
	)
	//  password flags
	cmd.Flags().StringVar(
		c.connectionOptions.TargetDB.Password,
		passwordFileFlag,
		"",
		"The absolute path to a file containing the password to the target database.",
	)
	cmd.Flags().StringVar(
		&globals.connFile,
		connFlag,
		"",
		"The absolute path to the connection file in yaml format.")
	markFlagsFileName(cmd, map[string][]string{connFlag: {"yaml"}})
}

func (c *CmdCreateConnection) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)
	return c.validateParse(logger)
}

func (c *CmdCreateConnection) validateParse(logger vlog.Printer) error {
	if !filepath.IsAbs(globals.connFile) {
		filePathError := errors.New(
			"Invalid connection file path: " + globals.connFile + ". The connection file path must be absolute.")
		logger.Error(filePathError, "Connection file path error:")
		return filePathError
	}
	ext := filepath.Ext(globals.connFile)
	if ext != dotYaml && ext != dotYml {
		fileTypeError := errors.New("Invalid file type: " + ext + ". Only .yaml or .yml is allowed.")
		logger.Error(fileTypeError, "Connection file type error:")
		return fileTypeError
	}
	return c.ValidateParseBaseOptions(&c.connectionOptions.DatabaseOptions)
}

func (c *CmdCreateConnection) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	// write target db info to vcluster connection file
	err := writeConn(c.connectionOptions)
	if err != nil {
		return fmt.Errorf("failed to write the connection file: %w", err)
	}
	vcc.DisplayInfo("Successfully wrote the connection file in %s", globals.connFile)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance
func (c *CmdCreateConnection) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.connectionOptions.DatabaseOptions = *opt
}
