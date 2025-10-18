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

package vclusterops

import (
	"fmt"
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type VListPackagesOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	// PackageName specifies which packages to list:
	// "" or "all" = all packages (default)
	// "default" = only default packages
	// specific name = only that package
	PackageFilter string

	// Note: DBName is optional in DatabaseOptions
	// - If provided: Online mode - checks installation status from database
	// - If empty: Offline mode - lists from /opt/vertica/packages without status
}

func VListPackagesOptionsFactory() VListPackagesOptions {
	options := VListPackagesOptions{}
	options.DatabaseOptions.setDefaultValues()
	// PackageFilter defaults to empty string, which means "all" (API will use its default)
	return options
}

func (options *VListPackagesOptions) validateParseOptions(logger vlog.Printer) error {
	if options.DBName != "" {
		err := options.validateBaseOptions(ListPackagesCmd, logger)
		if err != nil {
			return err
		}
	}

	return nil
}

// resolve hostnames to be IPs
func (options *VListPackagesOptions) analyzeOptions() (err error) {
	// we analyze hostnames when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	// Hosts should be populated either from CLI or from config
	if len(options.Hosts) == 0 {
		return fmt.Errorf("no hosts available - provide --hosts flag or hosts in config file")
	}

	return nil
}

func (options *VListPackagesOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// isDatabaseNotFoundError checks if the error indicates a database was not found.
func isDatabaseNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "is running on host")
}

func (vcc VClusterCommands) VListPackages(options *VListPackagesOptions) (*ListPackageStatus, error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return nil, err
	}

	// Generate the instructions and a pointer to the status object that will
	// get filled in when we run the instructions.
	instructions, status, err := vcc.produceListPackagesInstructions(options)
	if err != nil {
		return nil, fmt.Errorf("fail to production instructions: %w", err)
	}

	// Create a VClusterOpEngine, and add certs in case the Vertica HTTPS service
	// is configured to require certs.
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		if options.DBName != "" && isDatabaseNotFoundError(runError) {
			vcc.DisplayWarning("Database '%s' not found or not accessible.", options.DBName)

			// Retry without database - just list from filesystem
			vcc.Log.PrintInfo("Retrying without database connection to list available packages")
			options.DBName = ""
			return vcc.VListPackages(options)
		}
		return nil, fmt.Errorf("fail to list packages: %w", runError)
	}

	return status, nil
}

// produceListPackagesInstructions will build a list of instructions to execute for
// the list packages operation. It will return a status object that gets
// filled in when the instructions are run.
//
// The generated instructions are as follows:
//   - If database provided: Get up nodes through https call
//   - List packages using GET /packages endpoint
func (vcc *VClusterCommands) produceListPackagesInstructions(opts *VListPackagesOptions) ([]clusterOp, *ListPackageStatus, error) {
	var instructions []clusterOp
	var listOp httpsListPackagesOp

	// Determine mode based on whether database name is provided
	// checkStatus = true:  Online mode - check installation status from database
	// checkStatus = false: Offline mode - list from /opt/vertica/packages only
	checkStatus := (opts.DBName != "")

	if checkStatus {
		// With Database: get up nodes first, then list with status
		usePassword := false
		if opts.Password != nil {
			usePassword = true
			err := opts.validateUserName(vcc.Log)
			if err != nil {
				return nil, nil, err
			}
		}

		httpsGetUpNodesOp, err := makeHTTPSGetUpNodesOp(opts.DBName, opts.Hosts,
			usePassword, opts.UserName, opts.Password, ListPackagesCmd)
		if err != nil {
			return nil, nil, err
		}

		var noHosts = []string{}
		listOp, err = makeHTTPSListPackagesOp(noHosts, usePassword, opts.UserName,
			opts.Password, opts.PackageFilter, checkStatus)
		if err != nil {
			return nil, nil, err
		}

		instructions = []clusterOp{
			&httpsGetUpNodesOp,
			&listOp,
		}
	} else {
		// Without database: List from first available host without status
		if len(opts.Hosts) == 0 {
			return nil, nil, fmt.Errorf("no hosts available for listing packages")
		}
		var err error
		listOp, err = makeHTTPSListPackagesOp([]string{opts.Hosts[0]}, false, "", nil,
			opts.PackageFilter, checkStatus)
		if err != nil {
			return nil, nil, err
		}
		instructions = []clusterOp{&listOp}
	}

	return instructions, &listOp.status, nil
}
