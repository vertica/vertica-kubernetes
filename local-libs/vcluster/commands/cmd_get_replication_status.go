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
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdGetReplicationStatus
 *
 * Implements ClusterCommand interface
 */
type CmdGetReplicationStatus struct {
	replicationStatusOptions *vclusterops.VReplicationStatusDatabaseOptions
	CmdBase
	targetPasswordFile string
}

func makeCmdGetReplicationStatus() *cobra.Command {
	newCmd := &CmdGetReplicationStatus{}
	opt := vclusterops.VReplicationStatusFactory()
	newCmd.replicationStatusOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		replicationStatusSubCmd,
		"Get the status of an asynchronous replication job.",
		`Get the status of an asynchronous replication job

Examples:
  # Get replication status with connection file
  vcluster replication status --config --target-conn /opt/vertica/config/target_connection.yaml \
    --transaction-id 12345678901234567

  # Get replication status without connection file
  # option and password-based authentication 
  vcluster replication status --target-db-name platform_db --target-hosts 10.20.30.43 \
    --target-db-user dbadmin --target-password-file /path/to/password-file \
    --transaction-id 12345678901234567
`,
		[]string{outputFileFlag, targetIPv6Flag, targetHostsFlag, targetUserNameFlag, targetPasswordFileFlag, targetConnFlag,
			targetKeyFileFlag, targetCertFileFlag, targetCaCertFileFlag, targetTLSModeFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// Must provide a connection file or target database/hosts/credentials arguments
	markFlagsOneRequired(cmd, []string{targetConnFlag, targetDBNameFlag})
	markFlagsOneRequired(cmd, []string{targetConnFlag, targetHostsFlag})
	markFlagsOneRequired(cmd, []string{targetConnFlag, targetUserNameFlag})
	markFlagsOneRequired(cmd, []string{targetConnFlag, targetPasswordFileFlag})

	markFlagsRequired(cmd, transactionIDFlag)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdGetReplicationStatus) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.replicationStatusOptions.TargetDB.DBName,
		targetDBNameFlag,
		"",
		"The target database where data was replicated to.",
	)

	cmd.Flags().Int64Var(
		&c.replicationStatusOptions.TransactionID,
		transactionIDFlag,
		0,
		"[Required] The transaction ID of the asynchronous replication job output by the replication start command.",
	)
}

func (c *CmdGetReplicationStatus) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	return c.validateParse(logger)
}

// all validations of the arguments should go in here
func (c *CmdGetReplicationStatus) validateParse(logger vlog.Printer) error {
	logger.Info("Called method validateParse()")
	if !c.usePassword() {
		err := c.getTargetCertFilesFromCertPaths(&c.replicationStatusOptions.TargetDB)
		if err != nil {
			return err
		}
	}

	err := c.parseTargetHostList()
	if err != nil {
		return err
	}

	err = c.parseTargetPassword()
	if err != nil {
		return err
	}

	return c.ValidateParseBaseTargetOptions(&c.replicationStatusOptions.TargetDB)
}

func (c *CmdGetReplicationStatus) parseTargetHostList() error {
	if len(c.replicationStatusOptions.TargetDB.Hosts) == 0 {
		return fmt.Errorf("you must specify at least one target host")
	}

	err := util.ParseHostList(&c.replicationStatusOptions.TargetDB.Hosts)
	if err != nil {
		return fmt.Errorf("you must specify at least one target host")
	}
	return nil
}

func (c *CmdGetReplicationStatus) parseTargetPassword() error {
	options := c.replicationStatusOptions
	if !viper.IsSet(targetPasswordFileKey) {
		// reset password option to nil if password is not provided in cli
		options.TargetDB.Password = nil
		return nil
	}
	if c.replicationStatusOptions.TargetDB.Password == nil {
		options.TargetDB.Password = new(string)
	}

	if c.targetPasswordFile == "" {
		return fmt.Errorf("target password file path is empty")
	}
	password, err := c.passwordFileHelper(c.targetPasswordFile)
	if err != nil {
		return err
	}
	*options.TargetDB.Password = password
	return nil
}

func (c *CmdGetReplicationStatus) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.replicationStatusOptions

	replicationStatus, err := vcc.VReplicationStatus(options)
	if err != nil {
		vcc.LogError(err, "failed to get replication status", "targetDB", options.TargetDB.DBName)
		return err
	}

	// Output replication status as JSON
	if replicationStatus != nil {
		bytes, err := json.MarshalIndent(replicationStatus, "", "  ")
		if err != nil {
			return err
		}
		c.writeCmdOutputToFile(globals.file, bytes, vcc.GetLog())
		// If writing into stdout, add a new line
		if c.output == "" {
			fmt.Println("")
		}
	}

	vcc.DisplayInfo("Successfully retrieved replication status")
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance
func (c *CmdGetReplicationStatus) SetDatabaseOptions(_ *vclusterops.DatabaseOptions) {
	c.replicationStatusOptions.TargetDB.UserName = globals.targetUserName
	c.replicationStatusOptions.TargetDB.DBName = globals.targetDB
	c.replicationStatusOptions.TargetDB.Hosts = globals.targetHosts
	c.replicationStatusOptions.TargetDB.IPv6 = globals.targetIPv6
	c.targetPasswordFile = globals.targetPasswordFile
}
