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
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdListAllNodes
 *
 * Implements ClusterCommand interface
 */
type CmdReIP struct {
	reIPOptions  *vclusterops.VReIPOptions
	reIPFilePath string

	CmdBase
}

func makeCmdReIP() *cobra.Command {
	newCmd := &CmdReIP{}
	opt := vclusterops.VReIPFactory()
	newCmd.reIPOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		reIPSubCmd,
		"Updates the catalog with the IP addresses of your nodes when the database is stopped.",
		`Updates the catalog with the IP addresses of your nodes when the database is stopped.

You should run this command when the IP address for a node changes.

You should always stop the database before running re_ip.

The file specified by the --re-ip-file option must the absolute path to a
JSON file with the following format:
[
  {"from_address": "10.20.30.40", "to_address": "10.20.30.41"},
  {"from_address": "10.20.30.42", "to_address": "10.20.30.43"}
]

This file should only include the IP addresses of nodes that you want to update.
		
Examples:
  # Alter the IP address of database nodes with user input
  vcluster re_ip --db-name test_db --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
  	--catalog-path /data --re-ip-file /data/re_ip_map.json --sandbox sand \
    --password "PASSWORD"
  
  # Alter the IP address of database nodes with config file
  vcluster re_ip --db-name test_db --re-ip-file /data/re_ip_map.json \
    --config /opt/vertica/config/vertica_cluster.yaml \
    --password "PASSWORD"
`,
		[]string{dbNameFlag, hostsFlag, ipv6Flag, catalogPathFlag, configParamFlag, configFlag, sandboxFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require re-ip-file
	markFlagsRequired(cmd, reIPFileFlag)
	markFlagsFileName(cmd, map[string][]string{reIPFileFlag: {"json"}})

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdReIP) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.reIPFilePath,
		reIPFileFlag,
		"",
		"Path of the re-ip file",
	)
	cmd.Flags().StringVar(
		&c.reIPOptions.SandboxName,
		sandboxFlag,
		"",
		"The name of the sandbox. Required if the re-ip hosts are in a sandbox.",
	)
}

func (c *CmdReIP) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)
	// Set CheckDBRunning to true so that CLI can check running db for Re_IP
	// Re-IP should only be used for down DB, checking if db is running
	c.reIPOptions.CheckDBRunning = true
	return c.validateParse(logger)
}

func (c *CmdReIP) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	err := c.ValidateParseBaseOptions(&c.reIPOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	if !c.usePassword() {
		err = c.getCertFilesFromCertPaths(&c.reIPOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err = c.setConfigParam(&c.reIPOptions.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.reIPOptions.ReadReIPFile(c.reIPFilePath)
}

func (c *CmdReIP) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.reIPOptions

	// load config info from the YAML config file
	canUpdateConfig := true
	dbConfig, err := readConfig()
	if err != nil {
		vcc.LogInfo("Failed to read the configuration file: %v", err)
		canUpdateConfig = false
	}

	err = vcc.VReIP(options)
	if err != nil {
		vcc.LogError(err, "failed to re-ip nodes.")
		return err
	}

	vcc.DisplayInfo("Successfully updated the IP addresses of database nodes.")

	// update config file after running re_ip
	if canUpdateConfig {
		c.UpdateConfig(dbConfig)
		err = dbConfig.write(options.ConfigPath, true /*forceOverwrite*/)
		if err != nil {
			vcc.DisplayWarning("Failed to update configuration file: %v\n", err)
		}
	}

	// write config parameters to vcluster config param file
	err = c.writeConfigParam(options.ConfigurationParameters, true /*forceOverwrite*/)
	if err != nil {
		vcc.PrintWarning("Failed to write configuration param file: %s", err)
	}

	return nil
}

// UpdateConfig will update node addresses in the config object after re_ip
func (c *CmdReIP) UpdateConfig(dbConfig *DatabaseConfig) {
	nodeNameToAddress := make(map[string]string)
	for _, reIPInfo := range c.reIPOptions.ReIPList {
		if reIPInfo.TargetAddress != "" {
			nodeNameToAddress[reIPInfo.NodeName] = reIPInfo.TargetAddress
		}
	}

	for _, n := range dbConfig.Nodes {
		newAddress, ok := nodeNameToAddress[n.Name]
		if ok {
			n.Address = newAddress
		}
	}
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdReIP
func (c *CmdReIP) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.reIPOptions.DatabaseOptions = *opt
}
