package commands

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
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
