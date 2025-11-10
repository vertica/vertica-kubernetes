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

type VUninstallPackagesOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	// PackageFilter specifies which packages to uninstall:
	// "" or "all" = all packages (default)
	// "default" = only default packages
	// specific name = only that package
	// comma-separated list of specific names = those packages
	PackageFilter string
}

func VUninstallPackagesOptionsFactory() VUninstallPackagesOptions {
	options := VUninstallPackagesOptions{}
	options.DatabaseOptions.setDefaultValues()
	// PackageFilter defaults to empty string, which means "all" (API will use its default)
	return options
}

func (options *VUninstallPackagesOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(UninstallPackagesCmd, logger)
	if err != nil {
		return err
	}

	return nil
}

// resolve hostnames to be IPs
func (options *VUninstallPackagesOptions) analyzeOptions() (err error) {
	// we analyze hostnames when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VUninstallPackagesOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

func (vcc VClusterCommands) VUninstallPackages(options *VUninstallPackagesOptions) (*UninstallPackagesStatus, error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	vdb := makeVCoordinationDatabase()
	var initiatorHost []string

	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return nil, err
	}

	// TODO: Add sandbox support, when we support sandboxes for uninstalling packages
	// Attempt to get VDB info from running database (main cluster).
	// If this fails or DB is down, vdb.HostNodeMap will be empty.
	var connectionErr error
	err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, util.MainClusterSandbox)
	if err != nil {
		vcc.Log.Info("Unable to connect to database", "error", err)
		connectionErr = err
	}

	for host, vnode := range vdb.HostNodeMap {
		if vnode.State == util.NodeUpState {
			initiatorHost = []string{host}
			break
		}
	}

	if len(initiatorHost) == 0 {
		if connectionErr != nil {
			return nil, fmt.Errorf("cannot uninstall packages: failed to connect to database, details: %w", connectionErr)
		}
	}

	// Generate the instructions and a pointer to the status object that will
	// get filled in when we run the instructions.
	instructions, status, err := vcc.produceUninstallPackagesInstructions(options, initiatorHost)
	if err != nil {
		return nil, fmt.Errorf("fail to produce instructions: %w", err)
	}

	// Create a VClusterOpEngine, and add certs in case the Vertica HTTPS service
	// is configured to require certs.
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return nil, fmt.Errorf("fail to uninstall packages: %w", runError)
	}

	return status, nil
}

// produceUninstallPackagesInstructions will build a list of instructions to execute for
// the uninstall packages operation. It will return a status object that gets
// filled in when the instructions are run.
//
// The generated instructions are as follows:
func (vcc *VClusterCommands) produceUninstallPackagesInstructions(opts *VUninstallPackagesOptions,
	initiatorHost []string) ([]clusterOp, *UninstallPackagesStatus, error) {
	var instructions []clusterOp
	var status *UninstallPackagesStatus

	usePassword := false
	if opts.Password != nil {
		usePassword = true
		err := opts.validateUserName(vcc.Log)
		if err != nil {
			return nil, nil, err
		}
	}

	uninstallOp, err := makeHTTPSUninstallPackagesOp(initiatorHost, usePassword, opts.UserName,
		opts.Password, opts.PackageFilter)
	if err != nil {
		return nil, nil, err
	}

	instructions = []clusterOp{&uninstallOp}
	status = &uninstallOp.status

	return instructions, status, nil
}
