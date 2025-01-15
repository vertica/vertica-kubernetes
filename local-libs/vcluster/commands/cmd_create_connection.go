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
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

const (
	dotYaml           = ".yaml"
	dotYml            = ".yml"
	hostWarningMsgOne = "some hosts are invalid: "
	hostWarningMsgTwo = "We removed those invalid hosts from the connection file."
)

/* CmdCreateConnection
 *
 * Implements ClusterCommand interface
 */
type CmdCreateConnection struct {
	checkConnection   bool
	connectionOptions *vclusterops.DatabaseOptions
	CmdBase
}

func makeCmdCreateConnection() *cobra.Command {
	newCmd := &CmdCreateConnection{}
	options := vclusterops.DatabaseOptionsFactory()
	newCmd.connectionOptions = &options
	newCmd.connectionOptions.Password = new(string)

	cmd := makeBasicCobraCmd(
		newCmd,
		createConnectionSubCmd,
		"Creates a file with connection information for the target database.",
		`Creates a file with connection information for the target database. 
The generated connection file should be used with the replication command.

Examples:
  # create the connection file to /tmp/vertica_connection.yaml
  vcluster connection create --db-name platform_test_db --hosts 10.20.30.43 --db-user \ 
    dkr_dbadmin --password-file /tmp/password.txt --conn /tmp/vertica_connection.yaml
`,
		[]string{connFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)
	cmd.AddCommand(makeCheckConnectionCmd())
	markFlagsRequired(cmd, dbNameFlag, hostsFlag, passwordFileFlag, connFlag)
	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdCreateConnection) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.connectionOptions.DBName,
		dbNameFlag,
		"",
		"The name of the database. You should only use this option if you want to override the database name in your configuration file.",
	)
	cmd.Flags().StringSliceVar(
		&c.connectionOptions.RawHosts,
		hostsFlag,
		[]string{},
		"A comma-separated list of hosts in database.")
	cmd.Flags().StringVar(
		&c.connectionOptions.UserName,
		dbUserFlag,
		"",
		"The name of the user in the target database.",
	)
	cmd.Flags().StringVar(
		&c.passwordFile,
		passwordFileFlag,
		"",
		"The absolute path to a file containing the password to the target database.",
	)
	cmd.Flags().StringVar(
		&globals.connFile,
		connFlag,
		"",
		"The absolute path to the connection file in yaml format.")
	cmd.Flags().BoolVar(
		&c.checkConnection,
		"check-connection",
		false,
		"validate user inputs before creating the connection file",
	)
	markFlagsFileName(cmd, map[string][]string{connFlag: {"yaml"}})
}

func (c *CmdCreateConnection) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)
	err := c.setDBPassword(c.connectionOptions)
	if err != nil {
		return err
	}
	err = c.validateParse(logger)
	return err
}

func (c *CmdCreateConnection) validateParse(logger vlog.Printer) error {
	connFile := globals.connFile
	err := validateYamlFilePath(connFile, logger)
	if err != nil {
		return err
	}
	return c.ValidateParseBaseOptions(c.connectionOptions)
}

func (c *CmdCreateConnection) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")
	if c.checkConnection {
		invalidHosts, err := c.verifyAndUpdateConnectionOptions(vcc)
		if err != nil {
			vcc.DisplayError(converErrorMessage(err, vcc.GetLog()))
			return nil
		}
		if len(invalidHosts) > 0 {
			vcc.DisplayWarning(hostWarningMsgOne + strings.Join(invalidHosts, ",") + " " + hostWarningMsgTwo)
		}
		vcc.DisplayInfo("Successfully verified connection parameters")
	} else {
		c.connectionOptions.Hosts = c.connectionOptions.RawHosts
	}
	// write target db info to vcluster connection file
	err := c.writeConn()
	if err != nil {
		vcc.DisplayError("failed to write the connection file: " + err.Error())
		return nil
	}
	vcc.DisplayInfo("Successfully wrote the connection file in %s", globals.connFile)
	return err
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance
func (c *CmdCreateConnection) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.connectionOptions.LogPath = opt.LogPath
}

func (c *CmdCreateConnection) verifyAndUpdateConnectionOptions(vcc vclusterops.ClusterCommands) ([]string, error) {
	fetchNodeDetailsOptions := vclusterops.VFetchNodesDetailsOptionsFactory()

	fetchNodeDetailsOptions.DatabaseOptions = *c.connectionOptions
	fetchNodeDetailsOptions.LogPath = c.connectionOptions.LogPath
	validHosts, invalidHosts, returnErr := fetchNodeDetails(vcc, &fetchNodeDetailsOptions)
	if len(validHosts) > 0 {
		c.connectionOptions.Hosts = validHosts
		return invalidHosts, nil
	}
	return invalidHosts, returnErr
}

// writeConn will save instructions for connecting to a database into a connection file.
func (c *CmdCreateConnection) writeConn() error {
	if globals.connFile == "" {
		return fmt.Errorf("conn path is empty")
	}
	dbConn := c.readTargetDBToDBConn()
	// write a connection file with the given target database info from create_connection
	err := dbConn.write(globals.connFile)
	return err
}

// readTargetDBToDBConn converts target database to DatabaseConnection
func (c *CmdCreateConnection) readTargetDBToDBConn() DatabaseConnection {
	targetDBconn := MakeTargetDatabaseConn()
	targetDBconn.TargetDBName = c.connectionOptions.DBName
	targetDBconn.TargetHosts = c.connectionOptions.Hosts
	targetDBconn.TargetPasswordFile = c.passwordFile
	targetDBconn.TargetDBUser = c.connectionOptions.UserName
	return targetDBconn
}
