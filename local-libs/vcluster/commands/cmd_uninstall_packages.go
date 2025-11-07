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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdUninstallPackages
 *
 * Parses arguments for VUninstallPackagesOptions to pass down to
 * VUninstallPackages.
 *
 * Implements ClusterCommand interface
 */

type CmdUninstallPackages struct {
	CmdBase
	unInstallPkgOpts *vclusterops.VUninstallPackagesOptions
}

func makeCmdUninstallPackages() *cobra.Command {
	// CmdUninstallPackages
	newCmd := &CmdUninstallPackages{}
	opt := vclusterops.VUninstallPackagesOptionsFactory()
	newCmd.unInstallPkgOpts = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		unInstallPkgSubCmd,
		"Uninstalls packages from the database.",
		`Uninstalls the packages from /opt/vertica/packages.

Examples:
  # Uninstall all available packages (default)
  vcluster uninstall_packages

  # Uninstall default packages
  vcluster uninstall_packages --package "default"

  # Uninstall all packages
  vcluster uninstall_packages --package "all"

  # Uninstall specific package
  vcluster uninstall_packages --package ComplexTypes

  # Uninstall multiple packages (comma-separated)
  vcluster uninstall_packages --package "ComplexTypes,kafka,logsearch"

  # Uninstall all packages using database and host name
  vcluster uninstall_packages --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --password <PASSWORD>
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, passwordFlag, outputFileFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdUninstallPackages) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.unInstallPkgOpts.PackageFilter,
		"package",
		"",
		"Filter packages: 'all' (default), 'default', specific package name, or comma-separated list of package names.",
	)
}

func (c *CmdUninstallPackages) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.unInstallPkgOpts.DatabaseOptions)

	return c.validateParse()
}

// all validations of the arguments should go in here
func (c *CmdUninstallPackages) validateParse() error {
	// validate package filter
	if err := util.ValidatePackageFilter(c.unInstallPkgOpts.PackageFilter); err != nil {
		return err
	}

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.unInstallPkgOpts.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.unInstallPkgOpts.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.unInstallPkgOpts.DatabaseOptions)
}

func (c *CmdUninstallPackages) Analyze(_ vlog.Printer) error {
	return nil
}

func (c *CmdUninstallPackages) Run(vcc vclusterops.ClusterCommands) error {
	options := c.unInstallPkgOpts

	packageUninstall, err := vcc.VUninstallPackages(options)
	if err != nil {
		vcc.LogError(err, "failed to uninstall packages")
		return err
	}

	// uninstall package in the JSON format
	bytes, err := json.MarshalIndent(packageUninstall, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal package uninstall: %w", err)
	}

	c.writeCmdOutputToFile(globals.file, bytes, vcc.GetLog())
	vcc.LogInfo("Uninstalled the packages: ", "packages", string(bytes))

	// Check if any packages failed
	// Note: "Skipped" is NOT a failure - it means package was already uninstalled
	hasFailures := false
	for _, pkg := range packageUninstall.Packages {
		if strings.HasPrefix(pkg.InstallStatus, "Failed") {
			hasFailures = true
			break
		}
	}

	// Display appropriate message
	if hasFailures {
		vcc.DisplayWarning("Package uninstall completed with some failures")
	} else {
		vcc.DisplayInfo("Package uninstall completed successfully")
	}

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdUninstallPackages
func (c *CmdUninstallPackages) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.unInstallPkgOpts.DatabaseOptions = *opt
}
