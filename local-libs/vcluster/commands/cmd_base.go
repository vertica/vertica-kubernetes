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
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

const (
	filePerm               = 0644
	configDirPerm          = 0755
	defConfigParamFileName = "config_param.json"
)

// all three tls modes enable TLS and present (client) cert if requested
const (
	tlsModeEnable     = "enable"      // skip validating peer (server) cert
	tlsModeVerifyCA   = "verify-ca"   // validate peer cert signer chain, skipping hostname validation
	tlsModeVerifyFull = "verify-full" // validate peer cert signer chain and hostname
)

/* CmdBase
 *
 * Basic/common fields of vcluster commands
 */
type CmdBase struct {
	argv   []string
	parser *pflag.FlagSet

	// for some commands like list_all_nodes, we want to allow the output to be written
	// to a file instead of being displayed in stdout. This is the file the output will
	// be written to
	output                 string
	configParamFile        string
	passwordFile           string
	readPasswordFromPrompt bool
}

// ValidateParseBaseOptions will validate and parse the required base options in each command
func (c *CmdBase) ValidateParseBaseOptions(opt *vclusterops.DatabaseOptions) error {
	// parse raw hosts
	if len(opt.RawHosts) > 0 {
		err := util.ParseHostList(&opt.RawHosts)
		if err != nil {
			return err
		}
	}

	// parse TLS mode.  vclusterops allows different behavior for NMA and HTTPS conns, but
	// for simplicity and lack of use case outside k8s, vcluster does not.
	err := validateParseTLSMode(opt, globals.tlsMode)
	if err != nil {
		return err
	}

	return nil
}

// ValidateParseBaseTargetOptions will validate and parse the required target options in each command
func (c *CmdBase) ValidateParseBaseTargetOptions(opt *vclusterops.DatabaseOptions) error {
	// parse raw hosts
	if len(opt.Hosts) > 0 {
		err := util.ParseHostList(&opt.Hosts)
		if err != nil {
			return err
		}
	}

	// parse TLS mode.  vclusterops allows different behavior for NMA and HTTPS conns, but
	// for simplicity and lack of use case outside k8s, vcluster does not.
	err := validateParseTLSMode(opt, globals.targetTLSMode)
	if err != nil {
		return err
	}

	return nil
}

func validateParseTLSMode(opt *vclusterops.DatabaseOptions, tlsMode string) error {
	if tlsMode != "" {
		switch tlsModeLower := strings.ToLower(tlsMode); tlsModeLower {
		case tlsModeEnable:
			opt.DoVerifyHTTPSServerCert = false
			opt.DoVerifyNMAServerCert = false
			opt.DoVerifyPeerCertHostname = false
		case tlsModeVerifyCA:
			opt.DoVerifyHTTPSServerCert = true
			opt.DoVerifyNMAServerCert = true
			opt.DoVerifyPeerCertHostname = false
		case tlsModeVerifyFull:
			opt.DoVerifyHTTPSServerCert = true
			opt.DoVerifyNMAServerCert = true
			opt.DoVerifyPeerCertHostname = true
		default:
			return fmt.Errorf("unrecognized TLS mode: %s. Allowed values are: '%s', '%s'",
				tlsMode, tlsModeEnable, tlsModeVerifyCA)
		}
	}

	return nil
}

// SetParser can assign a pflag parser to CmdBase
func (c *CmdBase) SetParser(parser *pflag.FlagSet) {
	c.parser = parser
}

// setCommonFlags is a helper function to let subcommands set some shared flags among them
func (c *CmdBase) setCommonFlags(cmd *cobra.Command, flags []string) {
	if len(flags) == 0 {
		return
	}
	c.setConfigFlags(cmd, flags)
	if util.StringInArray(passwordFlag, flags) {
		c.setPasswordFlags(cmd)
	}
	// log-path is a flag that all the subcommands need
	cmd.Flags().StringVarP(
		&dbOptions.LogPath,
		logPathFlag,
		"l",
		logPath,
		"Path location used for the debug logs",
	)
	markFlagsFileName(cmd, map[string][]string{logPathFlag: {"log"}})

	// verbose is a flag that all the subcommands need
	cmd.Flags().BoolVar(
		&globals.verbose,
		verboseFlag,
		false,
		"Whether show the details of VCluster run in the console",
	)

	// TLS related flags are allowed by all subcommands,
	// except for create_connection and manage_config show.
	if cmd.Name() != configShowSubCmd && cmd.Name() != createConnectionSubCmd {
		c.setTLSFlags(cmd)
	}

	if cmd.Name() == startReplicationSubCmd || cmd.Name() == replicationStatusSubCmd {
		c.setTargetDBFlags(cmd)
	}

	if util.StringInArray(outputFileFlag, flags) {
		cmd.Flags().StringVarP(
			&c.output,
			outputFileFlag,
			"o",
			"",
			"Write output to this file instead of stdout",
		)
	}
	if util.StringInArray(dbUserFlag, flags) {
		cmd.Flags().StringVar(
			&dbOptions.UserName,
			dbUserFlag,
			"",
			"The username for connecting to the database",
		)
	}
}

// setConfigFlags sets the config flag as well as all the common flags that
// can also be set with values from the config file
func (c *CmdBase) setConfigFlags(cmd *cobra.Command, flags []string) {
	if util.StringInArray(dbNameFlag, flags) {
		cmd.Flags().StringVarP(
			&dbOptions.DBName,
			dbNameFlag,
			"d",
			"",
			"The name of the database. You should only use this option if you want to override the database name in your configuration file.")
	}
	if util.StringInArray(configFlag, flags) {
		cmd.Flags().StringVarP(
			&dbOptions.ConfigPath,
			configFlag,
			"c",
			"",
			"The path to the config file. If a configuration file is present in the default location (automatically generated by create_db),\n"+
				"you do not need to specify this option.\n"+
				"Default: /opt/vertica/config/vertica_cluster.yaml")
		markFlagsFileName(cmd, map[string][]string{configFlag: {"yaml"}})
	}
	if util.StringInArray(hostsFlag, flags) {
		cmd.Flags().StringSliceVar(
			&dbOptions.RawHosts,
			hostsFlag,
			[]string{},
			"A comma-separated list of hosts in database.")
	}
	if util.StringInArray(catalogPathFlag, flags) {
		cmd.Flags().StringVar(
			&dbOptions.CatalogPrefix,
			catalogPathFlag,
			"",
			"The absolute path to the catalog directory.")
		markFlagsDirName(cmd, []string{catalogPathFlag})
	}
	if util.StringInArray(dataPathFlag, flags) {
		cmd.Flags().StringVar(
			&dbOptions.DataPrefix,
			dataPathFlag,
			"",
			"The absolute path to the data directory. This should be the same for all nodes in the database.")
		markFlagsDirName(cmd, []string{dataPathFlag})
	}
	if util.StringInArray(communalStorageLocationFlag, flags) {
		cmd.Flags().StringVar(
			&dbOptions.CommunalStorageLocation,
			communalStorageLocationFlag,
			"",
			util.GetEonFlagMsg("The absolute path of your communal storage location."))
	}
	if util.StringInArray(depotPathFlag, flags) {
		cmd.Flags().StringVar(
			&dbOptions.DepotPrefix,
			depotPathFlag,
			"",
			util.GetEonFlagMsg("The absolute path to depot directory."))
		markFlagsDirName(cmd, []string{depotPathFlag})
	}
	if util.StringInArray(ipv6Flag, flags) {
		cmd.Flags().BoolVar(
			&dbOptions.IPv6,
			ipv6Flag,
			false,
			"Whether the hosts use IPv6 addresses. Hostnames resolve to IPv4 by default.")
	}
	if util.StringInArray(eonModeFlag, flags) {
		cmd.Flags().BoolVar(
			&dbOptions.IsEon,
			eonModeFlag,
			false,
			util.GetEonFlagMsg("Whether the database is an Eon Mode database."))
	}
	if util.StringInArray(configParamFlag, flags) {
		cmd.Flags().StringToStringVar(
			&dbOptions.ConfigurationParameters,
			configParamFlag,
			map[string]string{},
			"A comma-separated list of *`PARAMETER`*`=`*`VALUE`* pairs.\n"+
				"Parameters specified with this option override the ones in configuration parameter files, if any,\n"+
				"and take the following parameters: AWSAuth, AWSEndpoint, AWSEneableHttps, AWSRegion")
		cmd.Flags().StringVar(
			&c.configParamFile,
			configParamFileFlag,
			"",
			"The absolute path to a file containing configuration parameters and their values.")
	}
}

// setTLSFlags sets the TLS options in global variables for later processing
// into vclusterops options.
func (c *CmdBase) setTLSFlags(cmd *cobra.Command) {
	// vcluster CLI reads certs into memory before calling vclusterops only
	// if non-default values are specified, which is why the defaults here
	// are "" despite being listed otherwise in the help messages.
	// Those defaults are used by vclusterops if no in-memory certs are provided.
	cmd.Flags().StringVar(
		&globals.keyFile,
		keyFileFlag,
		"",
		fmt.Sprintf("Path to the key file, the default value is %s", filepath.Join(vclusterops.CertPathBase, "{username}.key")),
	)
	markFlagsFileName(cmd, map[string][]string{keyFileFlag: {"key"}})

	cmd.Flags().StringVar(
		&globals.certFile,
		certFileFlag,
		"",
		fmt.Sprintf("Path to the cert file, the default value is %s", filepath.Join(vclusterops.CertPathBase, "{username}.pem")),
	)
	markFlagsFileName(cmd, map[string][]string{certFileFlag: {"pem", "crt"}})
	cmd.MarkFlagsRequiredTogether(keyFileFlag, certFileFlag)

	cmd.Flags().StringVar(
		&globals.caCertFile,
		caCertFileFlag,
		"",
		fmt.Sprintf("Path to the trusted CA cert file, the default value is %s", filepath.Join(vclusterops.CertPathBase, "rootca.pem")),
	)
	markFlagsFileName(cmd, map[string][]string{caCertFileFlag: {"pem", "crt"}})

	cmd.Flags().StringVar(
		&globals.tlsMode,
		tlsModeFlag,
		"",
		fmt.Sprintf("Mode for TLS validation. Allowed values '%s', '%s', and '%s'. Default value is '%s'.",
			tlsModeEnable, tlsModeVerifyCA, tlsModeVerifyFull, tlsModeEnable),
	)
}

func (c *CmdBase) setTargetDBFlags(cmd *cobra.Command) {
	cmd.Flags().StringSliceVar(
		&globals.targetHosts,
		targetHostsFlag,
		[]string{},
		"A comma-separated list of hosts in target database.",
	)
	cmd.Flags().StringVar(
		&globals.targetUserName,
		targetUserNameFlag,
		"",
		"The name of a user in the target database.",
	)
	cmd.Flags().StringVar(
		&globals.targetPasswordFile,
		targetPasswordFileFlag,
		"",
		"The absolute path to a file containing the password for the target database. ",
	)
	cmd.Flags().StringVar(
		&globals.connFile,
		targetConnFlag,
		"",
		"[Required] The absolute path to the connection file created with the create_connection command, "+
			"containing the database name, hosts, and password (if any) for the target database. "+
			"Alternatively, you can provide this information manually with --target-db-name, "+
			"--target-hosts, and --target-password-file",
	)
	markFlagsFileName(cmd, map[string][]string{targetConnFlag: {"yaml"}})

	cmd.Flags().StringVar(
		&globals.targetKeyFile,
		targetKeyFileFlag,
		"",
		fmt.Sprintf("Path to the key file for the target database, the default value is %s",
			filepath.Join(vclusterops.CertPathBase, "{username}.key")),
	)
	markFlagsFileName(cmd, map[string][]string{targetKeyFileFlag: {"key"}})

	cmd.Flags().StringVar(
		&globals.targetCertFile,
		targetCertFileFlag,
		"",
		fmt.Sprintf("Path to the cert file for the target database, the default value is %s",
			filepath.Join(vclusterops.CertPathBase, "{username}.pem")),
	)
	markFlagsFileName(cmd, map[string][]string{targetCertFileFlag: {"pem", "crt"}})
	cmd.MarkFlagsRequiredTogether(targetKeyFileFlag, targetCertFileFlag)

	cmd.Flags().StringVar(
		&globals.targetCaCertFile,
		targetCaCertFileFlag,
		"",
		fmt.Sprintf("Path to the trusted CA cert file for the target database, the default value is %s",
			filepath.Join(vclusterops.CertPathBase, "rootca.pem")),
	)
	markFlagsFileName(cmd, map[string][]string{caCertFileFlag: {"pem", "crt"}})

	cmd.Flags().StringVar(
		&globals.targetTLSMode,
		targetTLSModeFlag,
		"",
		fmt.Sprintf("Mode for TLS validation for the target database. "+
			"Allowed values '%s', '%s', and '%s'. Default value is '%s'.",
			tlsModeEnable, tlsModeVerifyCA, tlsModeVerifyFull, tlsModeEnable),
	)
	cmd.Flags().BoolVar(
		&globals.targetIPv6,
		targetIPv6Flag,
		false,
		"Whether the target hosts use IPv6 addresses. Hostnames resolve to IPv4 by default.")
}

func (c *CmdBase) initConfigParam() error {
	// We need to find the path to the config param. The order of precedence is as follows:
	// 1. Option
	// 2. Default locations
	//   a. /opt/vertica/config/config_param.json if running vcluster in /opt/vertica/bin
	//   b. $HOME/.config/vcluster/config_param.json otherwise
	//
	// If none of these things are true, then we run the cli without a config param file.

	if c.configParamFile != "" {
		return nil
	}

	// Pick a default config param file

	// If we are running vcluster from /opt/vertica/bin, we'll assume we
	// have installed the vertica package on this machine and so can assume
	// /opt/vertica/config exists too.
	vclusterExePath, err := os.Executable()
	if err != nil {
		return err
	}
	if vclusterExePath == defaultExecutablePath {
		if util.CheckPathExist(rpmConfDir) {
			c.configParamFile = fmt.Sprintf("%s/%s", rpmConfDir, defConfigParamFileName)
			return nil
		}
	}
	// Finally default to the .config directory in the users home. This is used
	// by many CLI applications.
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	// Ensure the config directory exists.
	path := filepath.Join(cfgDir, "vcluster")
	err = os.MkdirAll(path, configDirPerm)
	if err != nil {
		// Just abort if we don't have write access to the config path
		return err
	}
	c.configParamFile = fmt.Sprintf("%s/%s", path, defConfigParamFileName)
	return nil
}

// setConfigParam sets the configuration parameters from config param file
func (c *CmdBase) setConfigParam(opt *vclusterops.DatabaseOptions) error {
	err := c.initConfigParam()
	if err != nil {
		return err
	}

	if c.configParamFile == "" {
		return nil
	}
	configParam, err := c.getConfigParamFromFile(c.configParamFile)
	if err != nil {
		return err
	}
	for name, val := range configParam {
		// allow users to overwrite params in file with --config-param
		if _, ok := opt.ConfigurationParameters[name]; ok {
			continue
		}
		opt.ConfigurationParameters[name] = val
	}
	return nil
}

func (c *CmdBase) writeConfigParam(configParam map[string]string, forceOverwrite bool) error {
	if !c.parser.Changed(configParamFlag) {
		// no new config param specified, no need to write
		return nil
	}
	if c.configParamFile == "" {
		return fmt.Errorf("the configuration parameter file path is empty")
	}
	if util.CheckPathExist(c.configParamFile) && !forceOverwrite {
		return fmt.Errorf("the configuration parameter file %s already exists. To overwrite it, use --force-overwrite-file", c.configParamFile)
	}
	configParamBytes, err := json.Marshal(&configParam)
	if err != nil {
		return fmt.Errorf("failed to marshal the configuration parameters: %w", err)
	}
	err = os.WriteFile(c.configParamFile, configParamBytes, filePerm)
	if err != nil {
		return fmt.Errorf("failed to write the configuration parameters file: %w", err)
	}
	return nil
}

func (c *CmdBase) getConfigParamFromFile(configParamFile string) (map[string]string, error) {
	if !util.CheckPathExist(configParamFile) {
		return nil, nil
	}
	// Read config param from file
	configParamBytes, err := os.ReadFile(configParamFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration parameters from the file %q: %w", configParamFile, err)
	}

	var configParam map[string]string
	err = json.Unmarshal(configParamBytes, &configParam)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration parameters from the file %q: %w", configParamFile, err)
	}

	return configParam, nil
}

// setPasswordFlags sets all the password flags
func (c *CmdBase) setPasswordFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(
		dbOptions.Password,
		passwordFlag,
		"p",
		"",
		"The database password.",
	)
	cmd.Flags().StringVar(
		&c.passwordFile,
		passwordFileFlag,
		"",
		"The absolute path to a file containing the database password.\n"+
			"If you pass `-` (that is, '--password-file -'), the password is read from STDIN.",
	)
	cmd.Flags().BoolVar(
		&c.readPasswordFromPrompt,
		readPasswordFromPromptFlag,
		false,
		"Whether prompt the user to enter the password.",
	)
	cmd.MarkFlagsMutuallyExclusive([]string{passwordFlag, passwordFileFlag,
		readPasswordFromPromptFlag}...)
}

// ResetUserInputOptions reset password option to nil in each command
// if it is not provided in cli
func (c *CmdBase) ResetUserInputOptions(opt *vclusterops.DatabaseOptions) {
	if !c.parser.Changed(passwordFlag) {
		opt.Password = nil
	}
}

// setDBPassword sets the password option if one of the password flags
// is provided in the cli
func (c *CmdBase) setDBPassword(opt *vclusterops.DatabaseOptions) error {
	if !c.usePassword() {
		// reset password option to nil if password is not provided in cli
		opt.Password = nil
		return nil
	}

	if c.parser.Changed(passwordFlag) {
		// no-op, password has been set elsewhere,
		// through --password flag
		return nil
	}
	if opt.Password == nil {
		opt.Password = new(string)
	}
	if c.readPasswordFromPrompt {
		password, err := readDBPasswordFromPrompt()
		if err != nil {
			return err
		}
		*opt.Password = password
		return nil
	}

	// hyphen(`-`) is used to indicate that input should come
	// from stdin rather than from a file
	if c.passwordFile == "-" {
		password, err := readFromStdin()
		if err != nil {
			return err
		}
		*opt.Password = strings.TrimSuffix(password, "\n")
		return nil
	}

	if c.passwordFile == "" {
		return fmt.Errorf("the password file path is empty")
	}
	password, err := c.passwordFileHelper(c.passwordFile)
	if err != nil {
		return err
	}
	*opt.Password = password
	return nil
}

func (c *CmdBase) passwordFileHelper(passwordFile string) (string, error) {
	// Read password from file
	passwordBytes, err := os.ReadFile(passwordFile)
	if err != nil {
		return "", fmt.Errorf("failed to read password from file %q: %w", passwordFile, err)
	}
	// Convert bytes to string, removing any newline characters
	return strings.TrimSuffix(string(passwordBytes), "\n"), nil
}

// usePassword returns true if at least one of the password
// flags is passed in the cli
func (c *CmdBase) usePassword() bool {
	return c.parser.Changed(passwordFlag) ||
		c.parser.Changed(passwordFileFlag) ||
		c.parser.Changed(readPasswordFromPromptFlag)
}

// writeCmdOutputToFile if output-file is set, writes the output of the command
// to a file, otherwise to stdout
func (c *CmdBase) writeCmdOutputToFile(f *os.File, output []byte, logger vlog.Printer) {
	_, err := f.Write(output)
	if err != nil {
		if f == os.Stdout {
			logger.DisplayWarning("%s", err)
		} else {
			logger.DisplayWarning("Could not write command output to file %s, details: %s", c.output, err)
		}
	}
}

// initCmdOutputFile returns the open file descriptor, that will
// be used to write the command output, or stdout
func (c *CmdBase) initCmdOutputFile() (*os.File, error) {
	if !c.parser.Changed(outputFileFlag) {
		return os.Stdout, nil
	}
	if c.output == "" {
		return nil, fmt.Errorf("output-file cannot be empty")
	}
	return os.OpenFile(c.output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
}

// getCertFilesFromPaths will update cert and key file from cert path options
func (c *CmdBase) getCertFilesFromCertPaths(opt *vclusterops.DatabaseOptions) error {
	err := getCertFilesFromCertPaths(opt, globals.certFile, globals.keyFile, globals.caCertFile)
	if err != nil {
		return err
	}

	return nil
}

// getTargetCertFilesFromPaths will update target cert and key file from cert path options
func (c *CmdBase) getTargetCertFilesFromCertPaths(opt *vclusterops.DatabaseOptions) error {
	err := getCertFilesFromCertPaths(opt, globals.targetCertFile, globals.targetKeyFile, globals.targetCaCertFile)
	if err != nil {
		return err
	}

	return nil
}

// getCertFilesFromPaths will update cert and key file from cert path options
func getCertFilesFromCertPaths(opt *vclusterops.DatabaseOptions,
	certFile string, keyFile string, caCertFile string) error {
	// TODO don't make this conditional on not using a PW for auth (see callers)
	if certFile != "" {
		certData, err := os.ReadFile(certFile)
		if err != nil {
			return fmt.Errorf("failed to read certificate file: %w", err)
		}
		opt.Cert = string(certData)
	}
	if keyFile != "" {
		keyData, err := os.ReadFile(keyFile)
		if err != nil {
			return fmt.Errorf("failed to read private key file: %w", err)
		}
		opt.Key = string(keyData)
	}
	if caCertFile != "" {
		caCertData, err := os.ReadFile(caCertFile)
		if err != nil {
			return fmt.Errorf("failed to read trusted CA certificate file: %w", err)
		}
		opt.CaCert = string(caCertData)
	}
	return nil
}
