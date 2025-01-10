package commands

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
	"github.com/vertica/vcluster/vclusterops"
	"gopkg.in/yaml.v3"
)

type DatabaseConnection struct {
	TargetPasswordFile string   `yaml:"targetPasswordFile" mapstructure:"targetPasswordFile"`
	TargetHosts        []string `yaml:"targetHosts" mapstructure:"targetHosts"`
	TargetDBName       string   `yaml:"targetDBName" mapstructure:"targetDBName"`
	TargetDBUser       string   `yaml:"targetDBUser" mapstructure:"targetDBUser"`
}

func MakeTargetDatabaseConn() DatabaseConnection {
	return DatabaseConnection{}
}

// loadConnToViper can fill viper keys using the connection file
func loadConnToViper() error {
	// read connection file and merge it into viper
	viper.SetConfigFile(globals.connFile)
	err := viper.MergeInConfig()
	if err != nil {
		fmt.Printf("Warning: fail to merge connection file %q for viper: %v\n", globals.connFile, err)
	}

	return nil
}

// writeConn will save instructions for connecting to a database into a connection file.
func writeConn(targetdb *vclusterops.VReplicationDatabaseOptions) error {
	if globals.connFile == "" {
		return fmt.Errorf("conn path is empty")
	}

	dbConn := readTargetDBToDBConn(targetdb)

	// write a connection file with the given target database info from create_connection
	err := dbConn.write(globals.connFile)
	if err != nil {
		return err
	}

	return nil
}

// readTargetDBToDBConn converts target database to DatabaseConnection
func readTargetDBToDBConn(cnn *vclusterops.VReplicationDatabaseOptions) DatabaseConnection {
	targetDBconn := MakeTargetDatabaseConn()
	targetDBconn.TargetDBName = cnn.TargetDB.DBName
	targetDBconn.TargetHosts = cnn.TargetDB.Hosts
	targetDBconn.TargetPasswordFile = *cnn.TargetDB.Password
	targetDBconn.TargetDBUser = cnn.TargetDB.UserName
	return targetDBconn
}

// write writes connection information to connFilePath. It returns
// any write error encountered. The viper in-built write function cannot
// work well (the order of keys cannot be customized) so we used yaml.Marshal()
// and os.WriteFile() to write the connection file.
func (c *DatabaseConnection) write(connFilePath string) error {
	configBytes, err := yaml.Marshal(*c)
	if err != nil {
		return fmt.Errorf("fail to marshal connection data, details: %w", err)
	}
	err = os.WriteFile(connFilePath, configBytes, configFilePerm)
	if err != nil {
		return fmt.Errorf("fail to write connection file, details: %w", err)
	}
	return nil
}
