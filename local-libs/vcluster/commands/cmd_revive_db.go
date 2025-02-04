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
	"strconv"

	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdReviveDB
 *
 * Implements ClusterCommand interface
 */
type CmdReviveDB struct {
	CmdBase
	reviveDBOptions *vclusterops.VReviveDatabaseOptions
}

func makeCmdReviveDB() *cobra.Command {
	// CmdReviveDB
	newCmd := &CmdReviveDB{}
	opt := vclusterops.VReviveDBOptionsFactory()
	newCmd.reviveDBOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		reviveDBSubCmd,
		"Revive or restores an Eon Mode database.",
		`Revives or restores an Eon Mode database. In a cluster with sandboxes, the database can be revived 
		 to main cluster by default or by using the arg --main-cluster-only. arg --sandbox <sandboxname> can 
		 be used to revive database to given sandbox.

If access to communal storage requires access keys, you must provide the keys with the --config-param option.

Examples:
  # Revive a database with user input and save the generated config file
  # under the given directory
  vcluster revive_db --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --communal-storage-location /communal \
    --config /opt/vertica/config/vertica_cluster.yaml \
    --password "PASSWORD"

  # Describe the database only when reviving the database
  vcluster revive_db --db-name test_db --communal-storage-location /communal \
    --display-only \
    --password "PASSWORD"

  # Revive a database with user input by restoring to a given restore point
  vcluster revive_db --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --communal-storage-location /communal \
    --config /opt/vertica/config/vertica_cluster.yaml --force-removal \
    --ignore-cluster-lease --restore-point-archive db --restore-point-index 1 \
    --password "PASSWORD"

`,
		[]string{dbNameFlag, hostsFlag, ipv6Flag, communalStorageLocationFlag, configFlag, outputFileFlag, configParamFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require db-name and communal-storage-location
	markFlagsRequired(cmd, dbNameFlag, communalStorageLocationFlag)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdReviveDB) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().UintVar(
		&c.reviveDBOptions.LoadCatalogTimeout,
		"load-catalog-timeout",
		util.DefaultLoadCatalogTimeoutSeconds,
		"The timeout, in seconds, for loading the remote catalog. Default: "+
			strconv.Itoa(util.DefaultLoadCatalogTimeoutSeconds),
	)
	cmd.Flags().BoolVar(
		&c.reviveDBOptions.ForceRemoval,
		"force-removal",
		false,
		"Deletes any existing database directories before reviving, excluding user storage directories.",
	)
	cmd.Flags().BoolVar(
		&c.reviveDBOptions.DisplayOnly,
		"display-only",
		false,
		"Shows information about the database in communal storage. If you specify this option, you can omit --hosts.",
	)
	cmd.Flags().BoolVar(
		&c.reviveDBOptions.IgnoreClusterLease,
		"ignore-cluster-lease",
		false,
		"Do not check for the existence of other clusters running on shared storage.\n"+
			"If another system is using the same communal storage, using this option results in data corruption.",
	)
	cmd.Flags().StringVar(
		&c.reviveDBOptions.RestorePoint.Archive,
		"restore-point-archive",
		"",
		"Name of the restore archive to use for bootstrapping",
	)
	cmd.Flags().IntVar(
		&c.reviveDBOptions.RestorePoint.Index,
		"restore-point-index",
		0,
		"The index of the restore point in the restore archive to restore from. Restore point indexes are one-indexed.",
	)
	cmd.Flags().StringVar(
		&c.reviveDBOptions.RestorePoint.ID,
		"restore-point-id",
		"",
		"The identifier of the restore point in the restore archive.",
	)
	cmd.Flags().StringVar(
		&c.reviveDBOptions.Sandbox,
		sandboxFlag,
		"",
		"Name of the sandbox to revive",
	)
	cmd.Flags().BoolVar(
		&c.reviveDBOptions.MainCluster,
		"main-cluster-only",
		false,
		"Revive the database on main cluster, but do not touch any of the sandboxes",
	)
	// only one of restore-point-index or restore-point-id" will be required
	cmd.MarkFlagsMutuallyExclusive("restore-point-index", "restore-point-id")
}

func (c *CmdReviveDB) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	return c.validateParse(logger)
}

func (c *CmdReviveDB) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.reviveDBOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	if c.reviveDBOptions.Sandbox != "" && c.reviveDBOptions.MainCluster {
		return fmt.Errorf("sandbox and main_cluster_only are mutually exclusive")
	}

	if c.reviveDBOptions.Sandbox == "" && !c.reviveDBOptions.MainCluster {
		logger.DisplayWarning("neither --sandbox nor --main_cluster_only option is specified, proceeding to revive to main cluster")
	}

	// when --display-only is provided, we do not need to parse some base options like hostListStr
	if c.reviveDBOptions.DisplayOnly {
		return nil
	}

	err := c.ValidateParseBaseOptions(&c.reviveDBOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.setConfigParam(&c.reviveDBOptions.DatabaseOptions)
	if err != nil {
		return err
	}
	return nil
}

func (c *CmdReviveDB) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")
	dbInfo, vdb, err := vcc.VReviveDatabase(c.reviveDBOptions)
	if err != nil {
		vcc.LogError(err, "failed to revive the database", "DBName", c.reviveDBOptions.DBName)
		return err
	}

	if c.reviveDBOptions.DisplayOnly {
		c.writeCmdOutputToFile(globals.file, []byte(dbInfo), vcc.GetLog())
		vcc.LogInfo("Database details: ", "db-info", dbInfo)
		return nil
	}

	vcc.DisplayInfo("Successfully revived database %s", c.reviveDBOptions.DBName)

	// write db info to vcluster config file
	vdb.FirstStartAfterRevive = true

	// Read the config file
	dbConfig := MakeDatabaseConfig()
	dbConfigPtr, configErr := readConfig()
	if configErr != nil {
		// config file does not exist, neither main cluster nor sandbox has been revived yet.
		// overwrite the config file.
		err = c.overwriteConfig(vdb)
		if err != nil {
			vcc.DisplayWarning(err.Error())
			return nil
		}
	} else {
		// config file already exists. This could happen if we have partially revived the db(sandbox or main cluster) already
		// In this case, we update the existing config file instead of overwriting it.
		dbConfig = *dbConfigPtr
		UpdateDBConfig(vdb, &dbConfig, c.reviveDBOptions.Sandbox, c.reviveDBOptions.MainCluster)
		writeErr := dbConfig.write(c.reviveDBOptions.ConfigPath, true /*forceOverwrite*/)
		if writeErr != nil {
			vcc.DisplayWarning("Fail to update config file: %s", writeErr)
			return nil
		}
		err = c.writeConfigParam(c.reviveDBOptions.ConfigurationParameters, true /*forceOverwrite*/)
		if err != nil {
			vcc.DisplayWarning("Failed to write the configuration parameter file: %s", err)
		}
	}
	return nil
}

func (c *CmdReviveDB) overwriteConfig(vdb *vclusterops.VCoordinationDatabase) error {
	err := writeConfig(vdb, true /*forceOverwrite*/)
	if err != nil {
		return fmt.Errorf("failed to write the configuration file: %s", err)
	}
	// write config parameters to vcluster config param file
	err = c.writeConfigParam(c.reviveDBOptions.ConfigurationParameters, true /*forceOverwrite*/)
	if err != nil {
		return fmt.Errorf("failed to write the configuration parameter file: %s", err)
	}
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdReviveDB
func (c *CmdReviveDB) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.reviveDBOptions.DatabaseOptions = *opt
}
