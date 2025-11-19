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

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type VListPackagesOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	// PackageFilter specifies which packages to list:
	// "" or "all" = all packages (default)
	// "default" = only default packages
	// specific name = only that package
	PackageFilter string

	// Note: The command auto-detects if the database is running.
	// - If database is UP: Checks installation status from database catalog
	// - If database is DOWN: Lists packages from filesystem only (status: "unknown")
}

func VListPackagesOptionsFactory() VListPackagesOptions {
	options := VListPackagesOptions{}
	options.DatabaseOptions.setDefaultValues()
	// PackageFilter defaults to empty string, which means "all" (API will use its default)
	return options
}

func (options *VListPackagesOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(ListPackagesCmd, logger)
	if err != nil {
		return err
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

func (vcc VClusterCommands) VListPackages(options *VListPackagesOptions) (*ListPackageStatus, error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	vdb := makeVCoordinationDatabase()
	isOnlineMode := false
	var initiatorHost []string

	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return nil, err
	}

	// TODO: Add sandbox support, when we support sandboxes for listing packages
	// Attempt to get VDB info from running database (main cluster).
	// If this fails or DB is down, vdb.HostNodeMap will be empty and we'll use offline mode.
	err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, util.MainClusterSandbox)
	if err != nil {
		vcc.Log.Info("Unable to connect to database, will use offline mode", "error", err)
	}
	for host, vnode := range vdb.HostNodeMap {
		if vnode.Sandbox == util.MainClusterSandbox && vnode.State == util.NodeUpState {
			isOnlineMode = true
			initiatorHost = []string{host}
			vcc.Log.Info("Database is UP, will check package installation status")
			break
		}
	}

	// Generate the instructions and a pointer to the status object that will
	// get filled in when we run the instructions.
	instructions, status, err := vcc.produceListPackagesInstructions(options, isOnlineMode, initiatorHost)
	if err != nil {
		return nil, fmt.Errorf("fail to produce instructions: %w", err)
	}

	// Create a VClusterOpEngine, and add certs in case the Vertica HTTPS service
	// is configured to require certs.
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return nil, fmt.Errorf("fail to list packages: %w", runError)
	}

	return status, nil
}

// produceListPackagesInstructions will build a list of instructions to execute for
// the list packages operation. It will return a status object that gets
// filled in when the instructions are run.
//
// The generated instructions are as follows:
//   - If database is UP: List packages with installation status from database catalog
//   - If database is DOWN: List packages from filesystem only (no status)
func (vcc *VClusterCommands) produceListPackagesInstructions(opts *VListPackagesOptions,
	isOnlineMode bool,
	initiatorHost []string) ([]clusterOp, *ListPackageStatus, error) {
	var instructions []clusterOp
	var status *ListPackageStatus

	if isOnlineMode {
		// Online mode - use HTTPS endpoint
		usePassword := false
		if opts.Password != nil {
			usePassword = true
			err := opts.validateUserName(vcc.Log)
			if err != nil {
				return nil, nil, err
			}
		}

		listOp, err := makeHTTPSListPackagesOp(initiatorHost, usePassword, opts.UserName,
			opts.Password, opts.PackageFilter)
		if err != nil {
			return nil, nil, err
		}

		instructions = []clusterOp{&listOp}
		status = &listOp.status
	} else {
		// Offline mode - use NMA endpoint
		if len(opts.Hosts) == 0 {
			return nil, nil, fmt.Errorf("no hosts available for listing packages (set --hosts or configure hosts in config file)")
		}
		initiatorHost = []string{opts.Hosts[0]}

		vcc.Log.Info("Database not accessible, will list packages from filesystem only")

		// Create NMA operation for offline mode
		offlineStatus := &ListPackageStatus{}
		nmaListOp := makeNMAListPackagesOp(initiatorHost, opts.PackageFilter, &offlineStatus.Packages)

		instructions = []clusterOp{&nmaListOp}
		status = offlineStatus
	}

	return instructions, status, nil
}
