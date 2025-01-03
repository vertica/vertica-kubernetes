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

package vclusterops

import (
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type VInstallPackagesOptions struct {
	/* part 1: basic db info */
	DatabaseOptions

	// If true, the packages will be reinstalled even if they are already installed.
	ForceReinstall bool
}

func VInstallPackagesOptionsFactory() VInstallPackagesOptions {
	options := VInstallPackagesOptions{}
	options.DatabaseOptions.setDefaultValues()
	return options
}

func (options *VInstallPackagesOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(InstallPackagesCmd, logger)
	if err != nil {
		return err
	}

	return nil
}

// resolve hostnames to be IPs
func (options *VInstallPackagesOptions) analyzeOptions() (err error) {
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

func (options *VInstallPackagesOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

func (vcc VClusterCommands) VInstallPackages(options *VInstallPackagesOptions) (*InstallPackageStatus, error) {
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
	instructions, status, err := vcc.produceInstallPackagesInstructions(options)
	if err != nil {
		return nil, fmt.Errorf("fail to production instructions: %w", err)
	}

	// Create a VClusterOpEngine, and add certs in case the Vertica HTTPS service
	// is configured to require certs.
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return nil, fmt.Errorf("fail to install packages: %w", runError)
	}
	if len(status.Packages) == 0 {
		return nil, fmt.Errorf("did not flow back the install package status")
	}

	return status, nil
}

// produceInstallPackagesInstructions will build a list of instructions to execute for
// the install packages operation. It will return a status object that gets
// filled in when the instructions are run.
//
// The generated instructions are as follows:
//   - Get up nodes through https call
//   - Install packages using one of the up nodes
func (vcc *VClusterCommands) produceInstallPackagesInstructions(opts *VInstallPackagesOptions) ([]clusterOp, *InstallPackageStatus, error) {
	// when password is specified, we will use username/password to call https endpoints
	usePassword := false
	if opts.Password != nil {
		usePassword = true
		err := opts.validateUserName(vcc.Log)
		if err != nil {
			return nil, nil, err
		}
	}

	httpsGetUpNodesOp, err := makeHTTPSGetUpNodesOp(opts.DBName, opts.Hosts,
		usePassword, opts.UserName, opts.Password, InstallPackagesCmd)
	if err != nil {
		return nil, nil, err
	}

	var noHosts = []string{} // We pass in no hosts so that this op picks an up node from the previous call.
	verbose := false         // Silence verbose output as we will print package status at the end
	installOp, err := makeHTTPSInstallPackagesOp(noHosts, usePassword, opts.UserName, opts.Password, opts.ForceReinstall, verbose)
	if err != nil {
		return nil, nil, err
	}

	instructions := []clusterOp{
		&httpsGetUpNodesOp,
		&installOp,
	}

	return instructions, &installOp.status, nil
}
