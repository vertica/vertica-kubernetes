/*
 (c) Copyright [2023-2025] Open Text.
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
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdDropDB
 *
 * Implements ClusterCommand interface
 */
type CmdDropDB struct {
	dropDBOptions *vclusterops.VDropDatabaseOptions

	CmdBase
}

func makeCmdDropDB() *cobra.Command {
	newCmd := &CmdDropDB{}
	opt := vclusterops.VDropDatabaseOptionsFactory()
	newCmd.dropDBOptions = &opt

	// VER-92345 update the long description about the hosts option
	cmd := makeBasicCobraCmd(
		newCmd,
		dropDBSubCmd,
		"Drops a database",
		`Drops a stopped database. The effects this command has on your data differs slightly between database modes:

- Enterprise: Deletes the database data (including catalog, data, and depot directories) from all nodes.
- Eon: Deletes non-communal storage data. Dropped Eon Mode databases can be revived.

The data deleted by this operation cannot be recovered.

Examples:
  # Drop a database with config file
  vcluster drop_db --db-name test_db \
    --config /opt/vertica/config/vertica_cluster.yaml
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, catalogPathFlag, dataPathFlag, depotPathFlag},
	)

	// hide flags since we expect it to come from config file, not from user input
	cmd.Flags().BoolVar(
		&newCmd.dropDBOptions.RetainCatalogDir,
		retainCatalogDirFlag,
		false,
		"Retain existing catalog directories",
	)
	cmd.Flags().StringToStringVar(
		&newCmd.dropDBOptions.NodeNameToHost,
		nodeNameToHostFlag,
		map[string]string{},
		"A comma-separated list of node_name=ip_address pairs",
	)
	hideLocalFlags(cmd, []string{hostsFlag, catalogPathFlag, dataPathFlag, depotPathFlag,
		retainCatalogDirFlag, nodeNameToHostFlag})

	return cmd
}

func (c *CmdDropDB) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	return c.validateParse(logger)
}

func (c *CmdDropDB) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")
	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.dropDBOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	// build a map for host-directories
	if len(c.dropDBOptions.NodeNameToHost) > 0 {
		// we skip using the config file if --nodename-to-host is provided
		// this is in particular for the k8s operator, which doesn't have the config file
		c.buildHostVNodeMapWithoutConfig()
	} else {
		err := c.buildHostVNodeMap(logger)
		if err != nil {
			return err
		}
	}
	return c.ValidateParseBaseOptions(&c.dropDBOptions.DatabaseOptions)
}

func (c *CmdDropDB) buildHostVNodeMapWithoutConfig() {
	options := c.dropDBOptions
	dbName := options.DBName

	c.dropDBOptions.NodesToDrop = []vclusterops.VCoordinationNode{}
	for nodeName, host := range options.NodeNameToHost {
		vnode := vclusterops.MakeVCoordinationNode()
		vnode.Address = host
		vnode.Name = nodeName
		pathSuffix := nodeName + "_catalog"
		vnode.CatalogPath = filepath.Join(options.CatalogPrefix, dbName, pathSuffix)
		c.dropDBOptions.NodesToDrop = append(c.dropDBOptions.NodesToDrop, vnode)
	}
}

func (c *CmdDropDB) buildHostVNodeMap(logger vlog.Printer) error {
	dbConfig, err := readConfig()
	if err != nil {
		logger.Error(err, "drop_db can only be used when the configration file is available and readable")
		return err
	}
	c.dropDBOptions.NodesToDrop = []vclusterops.VCoordinationNode{}
	for _, n := range dbConfig.Nodes {
		vNode := vclusterops.MakeVCoordinationNode()
		vNode.Address = n.Address
		vNode.Name = n.Name
		vNode.CatalogPath = n.CatalogPath
		vNode.StorageLocations = append(vNode.StorageLocations, n.DataPath)
		if n.DepotPath != "" {
			vNode.DepotPath = n.DepotPath
		}
		c.dropDBOptions.NodesToDrop = append(c.dropDBOptions.NodesToDrop, vNode)
	}
	return nil
}

func (c *CmdDropDB) Run(vcc vclusterops.ClusterCommands) error {
	vcc.V(1).Info("Called method Run()")
	err := vcc.VDropDatabase(c.dropDBOptions)
	if err != nil {
		vcc.LogError(err, "failed to drop the database")
		return err
	}

	vcc.DisplayInfo("Successfully dropped database %s", c.dropDBOptions.DBName)
	// if the database is successfully dropped, the config file will be removed
	// if failed to remove it, we will ask users to manually do it
	err = removeConfig()
	if err != nil {
		vcc.DisplayWarning("Failed to remove the configuration file %q, "+
			"please remove it manually: %v", c.dropDBOptions.ConfigPath, err)
	}
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdDropDB
func (c *CmdDropDB) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.dropDBOptions.DatabaseOptions = *opt
}
