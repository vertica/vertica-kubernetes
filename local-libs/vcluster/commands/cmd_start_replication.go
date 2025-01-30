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

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdStartReplication
 *
 * Implements ClusterCommand interface
 */
type CmdStartReplication struct {
	startRepOptions *vclusterops.VReplicationDatabaseOptions
	CmdBase
	targetPasswordFile string
}

func makeCmdStartReplication() *cobra.Command {
	newCmd := &CmdStartReplication{}
	opt := vclusterops.VReplicationDatabaseFactory()
	newCmd.startRepOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		startReplicationSubCmd,
		"Starts database replication",
		`Replicates a table or schema from a source database to a target database. 
		
The options --target-db-user and --target-password-file can be omitted 
if any one of the following conditions are met:
  - The source database has EnableConnectCredentialForwarding enabled.
  - The target database uses trust authentication.

Examples:
  # Start database replication with config and connection file
  vcluster replication start --config /opt/vertica/config/vertica_cluster.yaml \
    --target-conn /opt/vertica/config/target_connection.yaml \
    --password "PASSWORD"

  # Replicate data from a sandbox in the source database to a target database
  # specified in the connection file.
  vcluster replication start --config /opt/vertica/config/vertica_cluster.yaml \
    --target-conn /opt/vertica/config/target_connection.yaml --sandbox sand \
    --password "PASSWORD"

  # Start database replication with user input and connection file
  vcluster replication start --db-name test_db --hosts 10.20.30.40 \
    --target-conn /opt/vertica/config/target_connection.yaml \
    --password "PASSWORD"

  # Start database replication with config and connection file
  # tls option and tls-based authentication
  vcluster replication start --config /opt/vertica/config/vertica_cluster.yaml \ 
    --key-file /path/to/key-file --cert-file /path/to/cert-file \
    --target-conn /opt/vertica/config/target_connection.yaml --source-tlsconfig test_tlsconfig 
  
  # Start database replication with user input
  # option and password-based authentication 
  vcluster replication start --db-name test_db --db-user dbadmin --hosts 10.20.30.40 --target-db-name platform_db \
    --target-hosts 10.20.30.43 --password-file /path/to/password-file --target-db-user dbadmin \ 
    --target-password-file /path/to/password-file
`,
		[]string{dbNameFlag, hostsFlag, ipv6Flag, configFlag, passwordFlag, dbUserFlag, eonModeFlag, connFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// either target dbname+hosts or connection file must be provided
	cmd.MarkFlagsOneRequired(targetConnFlag, targetDBNameFlag)
	cmd.MarkFlagsOneRequired(targetConnFlag, targetHostsFlag)

	// tableOrSchema or pattern can not be accepted together
	cmd.MarkFlagsMutuallyExclusive(tableOrSchemaNameFlag, includePatternFlag)
	cmd.MarkFlagsMutuallyExclusive(tableOrSchemaNameFlag, excludePatternFlag)

	// hide eon mode flag since we expect it to come from config file, not from user input
	hideLocalFlags(cmd, []string{eonModeFlag, asyncFlag, tableOrSchemaNameFlag,
		includePatternFlag, excludePatternFlag, targetNamespaceFlag})
	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdStartReplication) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.startRepOptions.TargetDB.DBName,
		targetDBNameFlag,
		"",
		"The target database to replicate to.",
	)
	cmd.Flags().StringVar(
		&c.startRepOptions.SandboxName,
		sandboxFlag,
		"",
		"The source sandbox to replicate from.",
	)
	cmd.Flags().StringVar(
		&c.startRepOptions.SourceTLSConfig,
		sourceTLSConfigFlag,
		"",
		"The TLS configuration to use when connecting to the target database.\n "+
			"This TLS configuration must also exist in the source database.",
	)
	cmd.Flags().BoolVar(
		&c.startRepOptions.Async,
		asyncFlag,
		false,
		"If set to true, will run the replicate operation asynchronously. "+
			"Default value is false.",
	)
	cmd.Flags().StringVar(
		&c.startRepOptions.TableOrSchemaName,
		tableOrSchemaNameFlag,
		"",
		"(only async replication)The object name we want to copy from the source side. The available"+
			" types are: namespace, schema, table. If this is omitted, the operator"+
			" will replicate all namespaces in the source database.",
	)
	cmd.Flags().StringVar(
		&c.startRepOptions.IncludePattern,
		includePatternFlag,
		"",
		"(only async replication)A string containing a wildcard pattern of the schemas and/or tables to"+
			"include in the replication. Namespace names must be front-qualified "+
			"with a period.",
	)
	cmd.Flags().StringVar(
		&c.startRepOptions.ExcludePattern,
		excludePatternFlag,
		"",
		"(only async replication)A string containing a wildcard pattern of the schemas and/or tables"+
			" to exclude from the set of tables matched by the include pattern. "+
			"Namespace names must be front-qualified with a period.",
	)
	cmd.Flags().StringVar(
		&c.startRepOptions.TargetNamespace,
		targetNamespaceFlag,
		"",
		"(only async replication)Namespace in the target database to which objects are replicated."+
			" The target namespace must have the same shard count as the source "+
			"namespace in the source cluster."+
			"If you do not specify a target namespace, objects are replicated to"+
			" a namespace with the same name as the source namespace. If no such"+
			" namespace exists in the target cluster, it is created with the same"+
			" name and shard count as the source namespace. You can only replicate"+
			" tables in the public schema to the default_namespace in the target"+
			" cluster.",
	)
}

func (c *CmdStartReplication) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.startRepOptions.DatabaseOptions)

	// replication only works for an Eon db
	// When eon mode cannot be found in config file, we set its value to true
	if !viper.IsSet(eonModeKey) {
		c.startRepOptions.IsEon = true
	}

	return c.validateParse(logger)
}

// all validations of the arguments should go in here
func (c *CmdStartReplication) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.startRepOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}
	err := c.getTargetCertFilesFromCertPaths(&c.startRepOptions.TargetDB)
	if err != nil {
		return err
	}

	err = c.parseTargetHostList()
	if err != nil {
		return err
	}

	err = c.parseTargetPassword()
	if err != nil {
		return err
	}

	err = c.ValidateParseBaseTargetOptions(&c.startRepOptions.TargetDB)
	if err != nil {
		return err
	}

	err = c.ValidateParseBaseOptions(&c.startRepOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	return c.setDBPassword(&c.startRepOptions.DatabaseOptions)
}

func (c *CmdStartReplication) parseTargetHostList() error {
	if len(c.startRepOptions.TargetDB.Hosts) > 0 {
		err := util.ParseHostList(&c.startRepOptions.TargetDB.Hosts)
		if err != nil {
			return fmt.Errorf("you must specify at least one target host to replicate to")
		}
	}
	return nil
}

func (c *CmdStartReplication) parseTargetPassword() error {
	options := c.startRepOptions
	if !viper.IsSet(targetPasswordFileKey) {
		// reset password option to nil if password is not provided in cli
		options.TargetDB.Password = nil
		return nil
	}
	if c.startRepOptions.TargetDB.Password == nil {
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

func (c *CmdStartReplication) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.startRepOptions

	transactionID, err := vcc.VReplicateDatabase(options)
	if err != nil {
		vcc.LogError(err, "failed to replicate to database", "targetDB", options.TargetDB.DBName)
		return err
	}

	if options.Async {
		vcc.DisplayInfo("Successfully started replication to database %s. Transaction ID: %d", options.TargetDB.DBName, transactionID)
	} else {
		vcc.DisplayInfo("Successfully replicated to database %s", options.TargetDB.DBName)
	}

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance
func (c *CmdStartReplication) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.startRepOptions.DatabaseOptions = *opt
	c.startRepOptions.TargetDB.UserName = globals.targetUserName
	c.startRepOptions.TargetDB.DBName = globals.targetDB
	c.startRepOptions.TargetDB.Hosts = globals.targetHosts
	c.startRepOptions.TargetDB.IPv6 = globals.targetIPv6
	c.targetPasswordFile = globals.targetPasswordFile
}
