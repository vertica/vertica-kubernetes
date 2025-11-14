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
	"regexp"

	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdListPackages
 *
 * Parses arguments for VListPackagesOptions to pass down to
 * VListPackages.
 *
 * Implements ClusterCommand interface
 */

const (
	PkgStatusYes = "Yes"
	PkgStatusNo  = "No"
)

// Package name validation: follows Vertica unquoted identifier rule
//
// Valid examples:
//   - "ComplexTypes"
//   - "package123"
//
// Invalid examples:
//   - "123package"    (starts with digit)
//   - "package-name"  (contains hyphen)
//   - "package name"  (contains space)
var packageNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_$]{0,127}$`)

type CmdListPackages struct {
	CmdBase
	listPkgOpts *vclusterops.VListPackagesOptions
}

func makeCmdListPackages() *cobra.Command {
	// CmdListPackages
	newCmd := &CmdListPackages{}
	opt := vclusterops.VListPackagesOptionsFactory()
	newCmd.listPkgOpts = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		listPkgSubCmd,
		"Lists available packages and their installation status.",
		`Lists all available packages and their installation status.

The command automatically detects if the database is running:
- If database is UP: Shows installation status for each package
- If database is DOWN: Lists available packages from filesystem only

Examples:
  # List all available packages
  vcluster list_packages

  # List all packages with installation status for a database
  vcluster list_packages --db-name test_db \
    --hosts 10.20.30.40,10.20.30.41,10.20.30.42 \
    --password <PASSWORD>

  # List specific package(s) with status
  vcluster list_packages \
    --package default \
    --config <config_file_path> \
    --password <PASSWORD>
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, passwordFlag, outputFileFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdListPackages) setLocalFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(
		&c.listPkgOpts.PackageFilter,
		"package",
		"",
		"Filter packages: 'all' (default), 'default', or specific package name.",
	)
}

func (c *CmdListPackages) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.listPkgOpts.DatabaseOptions)

	return c.validateParse()
}

// all validations of the arguments should go in here
func (c *CmdListPackages) validateParse() error {
	if c.listPkgOpts.PackageFilter != "" {
		filter := c.listPkgOpts.PackageFilter

		// Allow special keywords
		if filter != "all" && filter != "default" {
			// Validate package name follows Vertica identifier rules
			if !packageNameRegex.MatchString(filter) {
				return fmt.Errorf("invalid package filter: %s", c.listPkgOpts.PackageFilter)
			}
		}
	}

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.listPkgOpts.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.listPkgOpts.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.listPkgOpts.DatabaseOptions)
}

func (c *CmdListPackages) Analyze(_ vlog.Printer) error {
	return nil
}

func (c *CmdListPackages) Run(vcc vclusterops.ClusterCommands) error {
	options := c.listPkgOpts

	packageList, err := vcc.VListPackages(options)
	if err != nil {
		vcc.LogError(err, "failed to list packages")
		return err
	}

	// list package in the JSON format
	bytes, err := json.MarshalIndent(packageList, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal package list: %w", err)
	}

	c.writeCmdOutputToFile(globals.file, bytes, vcc.GetLog())
	vcc.LogInfo("Listed the packages: ", "packages", string(bytes))

	// Display message based on results
	if len(packageList.Packages) == 0 {
		vcc.DisplayWarning("No packages available")
	} else {
		vcc.DisplayInfo("Successfully listed %d packages", len(packageList.Packages))
	}

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdListPackages
func (c *CmdListPackages) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.listPkgOpts.DatabaseOptions = *opt
}
