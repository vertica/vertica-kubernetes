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

/* CmdUpgradeLicense
 *
 * Parses arguments to upgrade-license and calls
 * the high-level function for upgrade-license.
 *
 * Implements ClusterCommand interface
 */

type CmdUpgradeLicense struct {
	CmdBase
	upgradeLicenseOptions *vclusterops.VUpgradeLicenseOptions
}

func makeCmdUpgradeLicense() *cobra.Command {
	newCmd := &CmdUpgradeLicense{}
	opt := vclusterops.VUpgradeLicenseFactory()
	newCmd.upgradeLicenseOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		upgradeLicenseCmd,
		"Upgrade license.",
		`Upgrade license.

Examples:
  # Upgrade license
  vcluster upgrade_license --license-file LICENSE_FILE --license-host HOST_OF_LICENSE_FILE

  # Upgrade license with connecting using database password 
  vcluster upgrade_license --license-file LICENSE_FILE --license-host HOST_OF_LICENSE_FILE  --password "PASSWORD"
`,
		[]string{dbNameFlag, configFlag, passwordFlag,
			hostsFlag, ipv6Flag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// require license file path
	markFlagsRequired(cmd, licenseFileFlag)
	markFlagsRequired(cmd, licenseHostFlag)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdUpgradeLicense) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.upgradeLicenseOptions.LicenseFilePath,
		licenseFileFlag,
		"",
		"Absolute path of the license file.",
	)
	cmd.Flags().StringVar(
		&c.upgradeLicenseOptions.LicenseHost,
		licenseHostFlag,
		"",
		"The host the license file located on.",
	)
}

func (c *CmdUpgradeLicense) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogArgParse(&c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.upgradeLicenseOptions.DatabaseOptions)

	return c.validateParse(logger)
}

func (c *CmdUpgradeLicense) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	err := c.ValidateParseBaseOptions(&c.upgradeLicenseOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	if !c.usePassword() {
		err = c.getCertFilesFromCertPaths(&c.upgradeLicenseOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}
	err = c.setDBPassword(&c.upgradeLicenseOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	return nil
}

func (c *CmdUpgradeLicense) Analyze(logger vlog.Printer) error {
	logger.Info("Called method Analyze()")
	return nil
}

func (c *CmdUpgradeLicense) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.upgradeLicenseOptions

	err := vcc.VUpgradeLicense(options)
	if err != nil {
		vcc.LogError(err, "failed to upgrade license", "license file", options.LicenseFilePath)
		return err
	}

	vcc.DisplayInfo("Successfully upgraded license: %s", options.LicenseFilePath)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdUpgradeLicense
func (c *CmdUpgradeLicense) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.upgradeLicenseOptions.DatabaseOptions = *opt
}
