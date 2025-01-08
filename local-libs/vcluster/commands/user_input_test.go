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
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops"
	"gopkg.in/yaml.v3"
)

var tempConfigFilePath = filepath.Join(os.TempDir(), "test_vertica_cluster.yaml")

const configRecover = "vcluster manage_config recover --db-name test_db "

const ymlExt, yamlExt = ".yml", ".yaml"

const tmpFilePrefixPattern = "test-*"

func simulateVClusterCli(vclusterCmd string) error {
	// if no log file is given, the log will go to stdout
	dbOptions.LogPath = ""

	// convert the input string into a slice
	// extra spaces will be trimmed
	os.Args = strings.Fields(vclusterCmd)
	fmt.Println("")

	// simulate a VCluster CLI call
	log.Printf("Simulating VCluster CLI call %+v\n", os.Args)
	err := rootCmd.Execute()

	// reset os.Args
	os.Args = nil
	return err
}

func TestConfigRecover(t *testing.T) {
	err := simulateVClusterCli("vcluster manage_config recover")
	assert.ErrorContains(t, err, `required flag(s) "catalog-path", "db-name", "hosts" not set`)

	err = simulateVClusterCli("vcluster manage_config recover --db-name test_db")
	assert.ErrorContains(t, err, `required flag(s) "catalog-path", "hosts" not set`)

	err = simulateVClusterCli(configRecover + "--hosts 192.168.1.101")
	assert.ErrorContains(t, err, `required flag(s) "catalog-path" not set`)

	tempConfig, _ := os.Create(tempConfigFilePath)
	tempConfig.Close()
	defer os.Remove(tempConfigFilePath)

	err = simulateVClusterCli(configRecover + "--hosts 192.168.1.101 --catalog-path /data " +
		"--config " + tempConfigFilePath)
	assert.ErrorContains(t, err, "config file exists at "+tempConfigFilePath)
}

func TestManageConfig(t *testing.T) {
	// if none of recover or show provided, `vcluster manage_config` should succeed and show help message
	err := simulateVClusterCli("vcluster manage_config")
	assert.NoError(t, err)

	err = simulateVClusterCli("vcluster manage_config show recover")
	assert.ErrorContains(t, err, `unknown command "recover" for "vcluster manage_config show"`)
}

func TestManageReplication(t *testing.T) {
	// vcluster replication should succeed and show help message
	err := simulateVClusterCli("vcluster replication")
	assert.NoError(t, err)

	err = simulateVClusterCli("vcluster replication start test")
	assert.ErrorContains(t, err, `unknown command "test" for "vcluster replication start"`)
}

func TestAsyncReplicationErrorMessage(t *testing.T) {
	vcommand := vclusterops.VClusterCommands{}
	replicationDatabaseOptions := vclusterops.VReplicationDatabaseFactory()
	replicationDatabaseOptions.DBName = "db"
	replicationDatabaseOptions.Hosts = []string{"12.34.56.78"}
	replicationDatabaseOptions.IsEon = true
	password := "password"
	replicationDatabaseOptions.Password = &password
	replicationDatabaseOptions.TargetDB.Hosts = []string{"23.45.67.89"}
	replicationDatabaseOptions.TargetDB.DBName = "targetDb"
	replicationDatabaseOptions.TableOrSchemaName = ".ns1.s1.*"
	_, err := vcommand.VReplicateDatabase(&replicationDatabaseOptions)
	assert.ErrorContains(t, err, "not allowed in --table-or-schema-name. HINT:")
}

func TestCreateConnectionFileWrongFileType(t *testing.T) {
	// vertica_connection.txt will not be created and a unique name is not required
	var tempConnFilePath = filepath.Join(os.TempDir(), "vertica_connection.txt")
	err := simulateVClusterCli("vcluster create_connection --db-name test_db1 --conn " + tempConnFilePath + " --hosts 192.168.1.101")
	assert.ErrorContains(t, err, `Invalid file type`)
}

func TestCreateConnectionFileAbsolutePathChecking(t *testing.T) {
	var relativeConnFilePath = "vertica_connection.yaml"
	err := simulateVClusterCli("vcluster create_connection --db-name test_db2 --conn " + relativeConnFilePath + " --hosts vnode2")
	assert.ErrorContains(t, err, `Invalid connection file path`)
}

func TestCreateConnectionFileRightFileTypes(t *testing.T) {
	tempFile, err := os.CreateTemp("", tmpFilePrefixPattern+yamlExt)
	if tempFile != nil {
		defer os.Remove(tempFile.Name())
	}
	assert.NoError(t, err)

	err = simulateVClusterCli("vcluster create_connection --db-name test_db3 --conn " + tempFile.Name() + " --hosts vnode3")
	assert.NoError(t, err)

	tempFile, err = os.CreateTemp("", tmpFilePrefixPattern+ymlExt)
	if tempFile != nil {
		defer os.Remove(tempFile.Name())
	}
	assert.NoError(t, err)
	err = simulateVClusterCli("vcluster create_connection --db-name test_db4 --conn " + tempFile.Name() + " --hosts vnode4")
	assert.NoError(t, err)
}

func TestCreateConnection(t *testing.T) {
	tempFile, err := os.CreateTemp("", tmpFilePrefixPattern+yamlExt)
	assert.NoError(t, err)
	os.Remove(tempFile.Name()) // clean up before test starts
	dbName := "platform_test_db"
	hosts := "192.168.1.101"

	// vcluster create_connection should succeed
	err = simulateVClusterCli("vcluster create_connection --db-name " + dbName + " --hosts " + hosts +
		" --conn " + tempFile.Name())
	defer os.Remove(tempFile.Name()) // It may be possible for the simulate to create the file and return an error
	assert.NoError(t, err)

	// verify the file content
	file, err := os.Open(tempFile.Name())
	assert.NoError(t, err)
	defer file.Close()

	buf := make([]byte, 1024)
	n, err := file.Read(buf)
	assert.NoError(t, err)

	var dbConn DatabaseConnection
	err = yaml.Unmarshal([]byte(string(buf[:n])), &dbConn)
	assert.NoError(t, err)

	assert.Equal(t, dbName, dbConn.TargetDBName)
	assert.Equal(t, hosts, dbConn.TargetHosts[0])
}

func TestStartNode(t *testing.T) {
	// either --start or --start-hosts must be specified
	err := simulateVClusterCli("vcluster start_node")
	assert.ErrorContains(t, err, "at least one of the flags in the group [start start-hosts] is required")

	// --start should be followed with the key1=value1,key2=value2 format
	err = simulateVClusterCli("vcluster start_node --start host1")
	assert.ErrorContains(t, err, `"--start" flag: host1 must be formatted as key=value`)

	// --start-hosts should be used with the config file
	err = simulateVClusterCli("vcluster start_node --start-hosts host1")
	assert.ErrorContains(t, err, "--start-hosts can only be used when the configuration file is available")

	// --start or --start-hosts cannot be both specified
	err = simulateVClusterCli("vcluster start_node --start node1=host1 --start-hosts host1")
	assert.ErrorContains(t, err, "[start start-hosts] were all set")
}
