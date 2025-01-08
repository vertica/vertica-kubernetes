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
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdCheckConnection
 *
 * Implements ClusterCommand interface
 */
type CmdCheckConnection struct {
	connectionOptions *vclusterops.DatabaseOptions
	CmdBase
}

func makeCheckConnectionCmd() *cobra.Command {
	newCmd := &CmdCheckConnection{}
	opt := vclusterops.DatabaseOptionsFactory()
	newCmd.connectionOptions = &opt
	opt.Password = new(string)
	cmd := makeBasicCobraCmd(
		newCmd,
		checkConnectionSubCmd,
		"Validate a connection file",
		`Check if a connection file is a valid one. 
		
Examples:
  # Check a connection file
  vcluster connection check --conn /opt/vertica/config/target_connection.yaml 
`,
		[]string{connFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)
	markFlagsRequired(cmd, connFlag)
	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdCheckConnection) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&globals.connFile,
		connFlag,
		"",
		"The absolute path to the connection file in yaml format.")
}

func (c *CmdCheckConnection) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)
	logger.Info("Called Parse()")
	connFile := globals.connFile
	err := validateYamlFilePath(connFile, logger)
	if err != nil {
		return err
	}
	err = c.loadFromViper()
	if err != nil {
		return err
	}
	err = c.parseTargetHostList()
	if err != nil {
		return err
	}
	return c.ValidateParseBaseOptions(c.connectionOptions)
}

func (c *CmdCheckConnection) parseTargetHostList() error {
	if len(c.connectionOptions.Hosts) > 0 {
		err := util.ParseHostList(&c.connectionOptions.Hosts)
		if err != nil {
			return fmt.Errorf("you must specify at least one target host to replicate to")
		}
	}
	return nil
}

func (c *CmdCheckConnection) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")
	invalidHosts, err := c.verifyAndUpdateConnectionOptions(vcc)
	if err != nil {
		vcc.DisplayError(converErrorMessage(err, vcc.GetLog()))
		return nil
	}
	if len(invalidHosts) > 0 {
		vcc.DisplayWarning(hostWarningMsgOne + strings.Join(invalidHosts, ","))
	}
	vcc.DisplayInfo("Successfully verified connection file")
	return nil
}

func (c *CmdCheckConnection) verifyAndUpdateConnectionOptions(vcc vclusterops.ClusterCommands) ([]string, error) {
	fetchNodeDetailsOptions := vclusterops.VFetchNodesDetailsOptionsFactory()
	password, err := c.passwordFileHelper(c.passwordFile)
	if err != nil {
		return nil, err
	}
	fetchNodeDetailsOptions.DatabaseOptions = *c.connectionOptions
	fetchNodeDetailsOptions.LogPath = c.connectionOptions.LogPath
	fetchNodeDetailsOptions.DatabaseOptions.Password = &password
	validHosts, invalidHosts, returnErr := fetchNodeDetails(vcc, &fetchNodeDetailsOptions)
	if len(validHosts) > 0 {
		c.connectionOptions.Hosts = validHosts
		return invalidHosts, nil
	}
	return invalidHosts, returnErr
}

func (c *CmdCheckConnection) loadFromViper() error {
	err := setTargetDBOptionsUsingViper(targetDBNameFlag)
	if err != nil {
		return err
	}
	c.connectionOptions.DBName = globals.targetDB
	err = setTargetDBOptionsUsingViper(targetUserNameFlag)
	if err != nil {
		return err
	}
	c.connectionOptions.UserName = globals.targetUserName
	err = setTargetDBOptionsUsingViper(targetPasswordFileFlag)
	if err != nil {
		return err
	}
	c.passwordFile = globals.targetPasswordFile
	err = setTargetDBOptionsUsingViper(targetHostsFlag)
	if err != nil {
		return err
	}
	c.connectionOptions.RawHosts = globals.targetHosts
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance
func (c *CmdCheckConnection) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.connectionOptions.LogPath = opt.LogPath
}
