/*
 (c) Copyright [2024] Open Text.
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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"gopkg.in/yaml.v3"
)

const (
	vclusterConfigEnv = "VCLUSTER_CONFIG"
	// If no config file was provided, we will pick a default one. This is the
	// default file name that we'll use.
	defConfigFileName        = "vertica_cluster.yaml"
	currentConfigFileVersion = "1.0"
	configFilePerm           = 0644
	rpmConfDir               = "/opt/vertica/config"
	defaultConfigFilePath    = rpmConfDir + "/" + defConfigFileName
)

// Config is the struct of vertica_cluster.yaml
type Config struct {
	Version  string         `yaml:"configFileVersion"`
	Database DatabaseConfig `yaml:",inline"`
}

// DatabaseConfig contains basic information for operating a database
type DatabaseConfig struct {
	Name                    string        `yaml:"dbName" mapstructure:"dbName"`
	Nodes                   []*NodeConfig `yaml:"nodes" mapstructure:"nodes"`
	IsEon                   bool          `yaml:"eonMode" mapstructure:"eonMode"`
	CommunalStorageLocation string        `yaml:"communalStorageLocation" mapstructure:"communalStorageLocation"`
	Ipv6                    bool          `yaml:"ipv6" mapstructure:"ipv6"`
	FirstStartAfterRevive   bool          `yaml:"firstStartAfterRevive" mapstructure:"firstStartAfterRevive"`
}

// NodeConfig contains node information in the database
type NodeConfig struct {
	Name        string `yaml:"name" mapstructure:"name"`
	Address     string `yaml:"address" mapstructure:"address"`
	Subcluster  string `yaml:"subcluster" mapstructure:"subcluster"`
	CatalogPath string `yaml:"catalogPath" mapstructure:"catalogPath"`
	DataPath    string `yaml:"dataPath" mapstructure:"dataPath"`
	DepotPath   string `yaml:"depotPath" mapstructure:"depotPath"`
	Sandbox     string `yaml:"sandbox" mapstructure:"sandbox"` // Name of the sandbox the node belongs to
}

// MakeDatabaseConfig() can create an instance of DatabaseConfig
func MakeDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{}
}

// initConfig will initialize the dbOptions.ConfigPath field for the vcluster exe.
func initConfig() {
	vclusterExePath, err := os.Executable()
	cobra.CheckErr(err)
	// If running vcluster from /opt/vertica/bin, we will ensure
	// /opt/vertica/config exists before using it.
	const ensureOptVerticaConfigExists = true
	// If using the user config director ($HOME/.config), we will ensure the necessary dir exists.
	const ensureUserConfigDirExists = true
	initConfigImpl(vclusterExePath, ensureOptVerticaConfigExists, ensureUserConfigDirExists)
}

// initConfigImpl will initialize the dbOptions.ConfigPath field. It will make an
// attempt to figure out the best value. In certain circumstances, it may fail
// to have a config path at all. In that case dbOptions.ConfigPath will be left
// as an empty string.
func initConfigImpl(vclusterExePath string, ensureOptVerticaConfigExists, ensureUserConfigDirExists bool) {
	// We need to find the path to the config. The order of precedence is as follows:
	// 1. Option
	// 2. Environment variable
	// 3. Default locations
	//   a. /opt/vertica/config/vertica_config.yaml if running vcluster in /opt/vertica/bin
	//   b. $HOME/.config/vcluster/vertica_config.yaml otherwise
	//
	// If none of these things are true, then we run the cli without a config file.

	// If option is set, nothing else to do in here
	if dbOptions.ConfigPath != "" {
		return
	}

	// Check environment variable
	if dbOptions.ConfigPath == "" {
		val, ok := os.LookupEnv(vclusterConfigEnv)
		if ok && val != "" {
			dbOptions.ConfigPath = val
			return
		}
	}

	// Pick a default config file.

	// If we are running vcluster from /opt/vertica/bin, we'll assume we
	// have installed the vertica package on this machine and so can assume
	// /opt/vertica/config exists too.
	if vclusterExePath == defaultExecutablePath {
		_, err := os.Stat(rpmConfDir)
		if ensureOptVerticaConfigExists && err != nil {
			if os.IsNotExist(err) {
				err = nil
			}
			cobra.CheckErr(err)
		} else {
			dbOptions.ConfigPath = defaultConfigFilePath
			return
		}
	}

	// Finally default to the .config directory in the users home. This is used
	// by many CLI applications.
	cfgDir, err := os.UserConfigDir()
	cobra.CheckErr(err)

	// Ensure the config directory exists.
	path := filepath.Join(cfgDir, "vcluster")
	if ensureUserConfigDirExists {
		const configDirPerm = 0755
		err = os.MkdirAll(path, configDirPerm)
		if err != nil {
			// Just abort if we don't have write access to the config path
			return
		}
	}
	dbOptions.ConfigPath = fmt.Sprintf("%s/%s", path, defConfigFileName)
}

// loadConfigToViper can fill viper keys using vertica_cluster.yaml
func loadConfigToViper() error {
	// read config file
	viper.SetConfigFile(dbOptions.ConfigPath)
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Printf("Warning: fail to read configuration file %q for viper: %v\n", dbOptions.ConfigPath, err)
		return nil
	}

	// retrieve db info in viper
	dbConfig := MakeDatabaseConfig()
	err = viper.Unmarshal(&dbConfig)
	if err != nil {
		fmt.Printf("Warning: fail to unmarshal configuration file into DatabaseConfig: %v\n", err)
		return nil
	}

	// if we can read config file, check if dbName in user input matches the one in config file
	if viper.IsSet(dbNameKey) && dbConfig.Name != viper.GetString(dbNameKey) {
		return fmt.Errorf("database %q does not match name found in the configuration file %q", dbConfig.Name, viper.GetString(dbNameKey))
	}

	// hosts, catalogPrefix, dataPrefix, depotPrefix are special in config file,
	// they are the values in each node so they need extra process.
	if !viper.IsSet(hostsKey) {
		viper.Set(hostsKey, dbConfig.getHosts())
	}
	catalogPrefix, dataPrefix, depotPrefix := dbConfig.getPathPrefixes()
	if !viper.IsSet(catalogPathKey) {
		viper.Set(catalogPathKey, catalogPrefix)
	}
	if !viper.IsSet(dataPathKey) {
		viper.Set(dataPathKey, dataPrefix)
	}
	if !viper.IsSet(depotPathKey) {
		viper.Set(depotPathKey, depotPrefix)
	}
	return nil
}

// writeConfig can write database information to vertica_cluster.yaml.
// It will be called in the end of some subcommands that will change the db state.
func writeConfig(vdb *vclusterops.VCoordinationDatabase, forceOverwrite bool) error {
	if dbOptions.ConfigPath == "" {
		return fmt.Errorf("configuration file path is empty")
	}

	return WriteConfigToPath(vdb, dbOptions.ConfigPath, forceOverwrite)
}

func WriteConfigToPath(vdb *vclusterops.VCoordinationDatabase,
	configPath string,
	forceOverwrite bool) error {
	dbConfig, err := readVDBToDBConfig(vdb)
	if err != nil {
		return err
	}

	// update db config with the given database info
	err = dbConfig.write(configPath, forceOverwrite)
	if err != nil {
		return err
	}

	return nil
}

// removeConfig remove the config file vertica_cluster.yaml.
// It will be called in the end of drop_db subcommands.
func removeConfig() error {
	if dbOptions.ConfigPath == "" {
		return fmt.Errorf("configuration file path is empty")
	}

	// remove the old db config
	return os.Remove(dbOptions.ConfigPath)
}

// readVDBToDBConfig converts vdb to DatabaseConfig
func readVDBToDBConfig(vdb *vclusterops.VCoordinationDatabase) (DatabaseConfig, error) {
	dbConfig := MakeDatabaseConfig()
	// loop over HostList is needed as we want to preserve the order
	for _, host := range vdb.HostList {
		vnode, ok := vdb.HostNodeMap[host]
		if !ok {
			return dbConfig, fmt.Errorf("cannot find host %s from HostNodeMap", host)
		}

		nodeConfig := BuildNodeConfig(vnode, vdb)
		dbConfig.Nodes = append(dbConfig.Nodes, &nodeConfig)
	}

	dbConfig.IsEon = vdb.IsEon
	dbConfig.CommunalStorageLocation = vdb.CommunalStorageLocation
	dbConfig.Ipv6 = vdb.Ipv6
	dbConfig.Name = vdb.Name
	dbConfig.FirstStartAfterRevive = vdb.FirstStartAfterRevive

	return dbConfig, nil
}

func BuildNodeConfig(vnode *vclusterops.VCoordinationNode,
	vdb *vclusterops.VCoordinationDatabase) NodeConfig {
	nodeConfig := NodeConfig{}
	nodeConfig.Name = vnode.Name
	nodeConfig.Address = vnode.Address
	nodeConfig.Subcluster = vnode.Subcluster
	nodeConfig.Sandbox = vnode.Sandbox

	if vdb.CatalogPrefix == "" {
		nodeConfig.CatalogPath = vnode.CatalogPath
	} else {
		nodeConfig.CatalogPath = vdb.GenCatalogPath(vnode.Name)
	}
	if vdb.DataPrefix == "" && len(vnode.StorageLocations) > 0 {
		nodeConfig.DataPath = vnode.StorageLocations[0]
	} else {
		nodeConfig.DataPath = vdb.GenDataPath(vnode.Name)
	}
	if vdb.IsEon && vdb.DepotPrefix == "" {
		nodeConfig.DepotPath = vnode.DepotPath
	} else if vdb.DepotPrefix != "" {
		nodeConfig.DepotPath = vdb.GenDepotPath(vnode.Name)
	}

	return nodeConfig
}

// Update give node info based on give vnode info
func updateNodeConfig(vnode *vclusterops.VCoordinationNode,
	vdb *vclusterops.VCoordinationDatabase, n *NodeConfig) {
	n.Address = vnode.Address
	n.Subcluster = vnode.Subcluster
	n.Sandbox = vnode.Sandbox
	if n.CatalogPath == "" {
		if strings.HasSuffix(vnode.CatalogPath, "/Catalog") {
			// Remove the "/Catalog" suffix and assign the remaining path to catalogPath
			n.CatalogPath = strings.TrimSuffix(vnode.CatalogPath, "/Catalog")
		} else {
			n.CatalogPath = vnode.CatalogPath
		}
	}
	if vdb.DataPrefix == "" && len(vnode.StorageLocations) > 0 && n.DataPath == "" {
		n.DataPath = vnode.StorageLocations[0]
	}
	n.DepotPath = vnode.DepotPath
}

// update the input dbConfig
func UpdateDBConfig(vdb *vclusterops.VCoordinationDatabase, dbConfig *DatabaseConfig, sandbox string, mainClusterOnly bool) {
	var newNodes []*NodeConfig
	nodeConfigMap := make(map[string]*NodeConfig)
	for _, n := range dbConfig.Nodes {
		nodeConfigMap[n.Name] = n
	}

	for _, vnode := range vdb.HostNodeMap {
		if sandbox == vnode.Sandbox || (mainClusterOnly && vnode.Sandbox == util.MainClusterSandbox) {
			if n, exists := nodeConfigMap[vnode.Name]; exists {
				// If found, update the existing node configuration
				updateNodeConfig(vnode, vdb, n)
			} else {
				// If not found, build and append a new node configuration
				n := BuildNodeConfig(vnode, vdb)
				newNodes = append(newNodes, &n)
			}
		}
	}
	dbConfig.Nodes = append(dbConfig.Nodes, newNodes...)
	sort.Slice(dbConfig.Nodes, func(i, j int) bool {
		return dbConfig.Nodes[i].Name < dbConfig.Nodes[j].Name
	})
}

// read reads information from configFilePath to a DatabaseConfig object.
// It returns any read error encountered.
func readConfig() (dbConfig *DatabaseConfig, err error) {
	configFilePath := dbOptions.ConfigPath

	if configFilePath == "" {
		return nil, fmt.Errorf("configuration file path is empty")
	}
	configBytes, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("fail to read configuration file, details: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(configBytes, &config)
	if err != nil {
		return nil, fmt.Errorf("fail to unmarshal configuration file, details: %w", err)
	}

	return &config.Database, nil
}

// write writes configuration information to configFilePath. It returns
// any write error encountered. The viper in-built write function cannot
// work well(the order of keys cannot be customized) so we used yaml.Marshal()
// and os.WriteFile() to write the config file.
func (c *DatabaseConfig) write(configFilePath string, forceOverwrite bool) error {
	if util.CheckPathExist(configFilePath) && !forceOverwrite {
		return fmt.Errorf("file %s exist, consider using --force-overwrite-file to overwrite the file", configFilePath)
	}
	var config Config
	config.Version = currentConfigFileVersion
	config.Database = *c

	configBytes, err := yaml.Marshal(&config)
	if err != nil {
		return fmt.Errorf("fail to marshal configuration data, details: %w", err)
	}

	err = os.WriteFile(configFilePath, configBytes, configFilePerm)
	if err != nil {
		return fmt.Errorf("fail to write configuration file, details: %w", err)
	}

	return nil
}

// Exposing the write function for external packages
func (c *DatabaseConfig) Write(configFilePath string, forceOverwrite bool) error {
	return c.write(configFilePath, forceOverwrite)
}

// getHosts returns host addresses of all nodes in database
func (c *DatabaseConfig) getHosts() []string {
	var hostList []string

	for _, vnode := range c.Nodes {
		hostList = append(hostList, vnode.Address)
	}

	return hostList
}

// getPathPrefix returns catalog, data, and depot prefixes
func (c *DatabaseConfig) getPathPrefixes() (catalogPrefix string,
	dataPrefix string, depotPrefix string) {
	if len(c.Nodes) == 0 {
		return "", "", ""
	}

	return util.GetPathPrefix(c.Nodes[0].CatalogPath), util.GetPathPrefix(c.Nodes[0].DataPath), util.GetPathPrefix(c.Nodes[0].DepotPath)
}
