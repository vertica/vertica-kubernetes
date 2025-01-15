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
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdCreateDB
 *
 * Parses arguments to createDB and calls
 * the high-level function for createDB.
 *
 * Implements ClusterCommand interface
 */

type CmdCreateDB struct {
	createDBOptions *vclusterops.VCreateDatabaseOptions
	CmdBase
}

func makeCmdCreateDB() *cobra.Command {
	newCmd := &CmdCreateDB{}
	opt := vclusterops.VCreateDatabaseOptionsFactory()
	newCmd.createDBOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		createDBSubCmd,
		"Creates a database",
		`Creates a new database and its associated configuration file for use with other vcluster commands.

Examples:
  # Create a database and save the generated config file under custom directory
  vcluster create_db --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --catalog-path /data --data-path /data \
    --config-param HttpServerConf=/opt/vertica/config/https_certs/httpstls.json \
    --config $HOME/custom/directory/vertica_cluster.yaml \
    --password "PASSWORD"

  # Read the password from file
  vcluster create_db --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --catalog-path /data --data-path /data \
    --password-file /path/to/password-file

  # Generate a random password and read it from stdin
  cat /dev/urandom | tr -dc A-Za-z0-9 | head -c 8 | tee password.txt | vcluster create_db --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --catalog-path /data --data-path /data \
    --password-file -

  # Prompt the user to enter the password
  vcluster create_db --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --catalog-path /data --data-path /data \
    --read-password-from-prompt

  # Password passed as plain text
  vcluster create_db --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --catalog-path /data --data-path /data \
    --password "PASSWORD"
`,
		[]string{dbNameFlag, hostsFlag, catalogPathFlag, dataPathFlag, depotPathFlag,
			communalStorageLocationFlag, passwordFlag, configFlag, ipv6Flag, configParamFlag},
	)
	// local flags
	newCmd.setLocalFlags(cmd)

	// check if hidden flags can be implemented/removed in VER-92259
	// hidden flags
	newCmd.setHiddenFlags(cmd)

	// require db-name
	markFlagsRequired(cmd, dbNameFlag, hostsFlag, catalogPathFlag, dataPathFlag)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdCreateDB) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.createDBOptions.LicensePathOnNode,
		"license",
		"",
		"The absolute path to a license file.",
	)
	cmd.Flags().StringVar(
		&c.createDBOptions.Policy,
		"policy",
		util.DefaultRestartPolicy,
		"The restart policy of the database.",
	)
	cmd.Flags().StringVar(
		&c.createDBOptions.SQLFile,
		"sql",
		"",
		"The SQL file to run (as dbadmin) after database creation.",
	)
	markFlagsFileName(cmd, map[string][]string{"sql": {"sql"}})
	cmd.Flags().IntVar(
		&c.createDBOptions.ShardCount,
		"shard-count",
		0,
		util.GetEonFlagMsg("The number of shards in the database."),
	)
	cmd.Flags().StringVar(
		&c.createDBOptions.DepotSize,
		"depot-size",
		"",
		util.GetEonFlagMsg(util.DepotFmtMsg+util.DepotSizeKMGTMsg+util.DepotSizeHint),
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.GetAwsCredentialsFromEnv,
		"get-aws-credentials-from-env-vars",
		false,
		util.GetEonFlagMsg("Retrieves AWS credentials from the following environment variables: $AWS_ACCESS_KEY_ID, $AWS_SECRET_ACCESS_KEY"),
	)
	cmd.Flags().IntVar(
		&c.createDBOptions.LargeCluster,
		"large-cluster",
		-1,
		"Enables the large cluster layout and sets the number of control nodes (default: -1, disabled).\n"+
			"The effect of this option is slightly different on Enterprise and Eon databases. For details, see the Vertica documentation.",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.ForceCleanupOnFailure,
		"force-cleanup-on-failure",
		false,
		"Deletes directories created by create_db upon failure.",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.ForceRemovalAtCreation,
		"force-removal-at-creation",
		false,
		"Deletes existing database directories before attempting to create the database.",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.ForceOverwriteFile,
		"force-overwrite-file",
		false,
		"Overwrites the current configuration file, if any.",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.SkipPackageInstall,
		"skip-package-install",
		false,
		"Skips installing the packages in /opt/vertica/packages.",
	)
	cmd.Flags().IntVar(
		&c.createDBOptions.TimeoutNodeStartupSeconds,
		"startup-timeout",
		util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", util.DefaultTimeoutSeconds),
		"The time, in seconds, to wait for the nodes to start after database creation (default: 300).",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.EnableTLSAuth,
		enableTLSAuthFlag,
		false,
		"Enable TLS authentication for all users after database creation",
	)
	c.setSpreadlFlags(cmd)
}

func (c *CmdCreateDB) setSpreadlFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(
		&c.createDBOptions.SpreadLogging,
		"spread-logging",
		false,
		"Whether enable Spread logging.",
	)
	cmd.Flags().IntVar(
		&c.createDBOptions.SpreadLoggingLevel,
		"spread-logging-level",
		-1,
		"The Spread logging level.",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.P2p,
		"point-to-point",
		true,
		"Configures Spread to use point-to-point communication between all Vertica nodes (default: enabled).\n"+
			"You should use this option if your nodes are not on the same subnet and for virtual environments.\n"+
			"Do not combine this option with --broadcast.\n"+
			"Up to 80 Spread daemons are supported by point-to-point communication. You can exceed the 80-node limit by using large cluster mode,\n"+
			"which only installs the Spread daemon on a subset of your nodes.",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.Broadcast,
		"broadcast",
		false,
		"Configures Spread to use UDP broadcast traffic between nodes on the same subnet (default: disabled).\n"+
			"Do not combine this option with `--point-to-point`.\n"+
			"Up to 80 Spread daemons are supported by broadcast traffic. You can exceed the 80-node limit by using large cluster mode,\n"+
			"which does not install a Spread daemon on each node.",
	)
}

// setHiddenFlags will set the hidden flags the command has.
// These hidden flags will not be shown in help and usage of the command, and they will be used internally.
func (c *CmdCreateDB) setHiddenFlags(cmd *cobra.Command) {
	cmd.Flags().IntVar(
		&c.createDBOptions.ClientPort,
		"client-port",
		util.DefaultClientPort,
		"",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.SkipStartupPolling,
		"skip-startup-polling",
		false,
		"",
	)
	cmd.Flags().BoolVar(
		&c.createDBOptions.GenerateHTTPCerts,
		"generate-http-certs",
		false,
		"",
	)
	hideLocalFlags(cmd, []string{"policy", "sql", "client-port", "skip-startup-polling", "generate-http-certs"})
}

func (c *CmdCreateDB) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	if !c.parser.Changed(depotPathFlag) {
		c.createDBOptions.IsEon = false
	} else {
		c.createDBOptions.IsEon = true
	}

	return c.validateParse(logger)
}

// all validations of the arguments should go in here
func (c *CmdCreateDB) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	err := c.ValidateParseBaseOptions(&c.createDBOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	// for creating a database, db password is mandatory input
	// we need to read certs for connecting to node management agent
	err = c.getCertFilesFromCertPaths(&c.createDBOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.setDBPassword(&c.createDBOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.initConfigParam()
	if err != nil {
		return err
	}
	return nil
}

func (c *CmdCreateDB) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")
	vdb, createError := vcc.VCreateDatabase(c.createDBOptions)
	if createError != nil {
		return createError
	}

	vcc.DisplayInfo("Successfully created a database with name [%s]", vdb.Name)

	// write db info to vcluster config file
	err := writeConfig(&vdb, c.createDBOptions.ForceOverwriteFile)
	if err != nil {
		vcc.DisplayWarning("Failed to write the configuration file: %s", err)
		if dbOptions.ConfigPath != defaultConfigFilePath {
			vcc.DisplayWarning("Attempting writing to default config file path: %s", defaultConfigFilePath)
			dbOptions.ConfigPath = defaultConfigFilePath
			err = writeConfig(&vdb, c.createDBOptions.ForceOverwriteFile)
			if err != nil {
				vcc.DisplayWarning("Failed to write the configuration file to default path: %s", err)
			}
		}
	}

	// write config parameters to vcluster config param file
	err = c.writeConfigParam(c.createDBOptions.ConfigurationParameters, c.createDBOptions.ForceOverwriteFile)
	if err != nil {
		vcc.DisplayWarning("Failed to write configuration parameter file: %s", err)
	}
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdCreateDB
func (c *CmdCreateDB) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.createDBOptions.DatabaseOptions = *opt
}
