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
	"path/filepath"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

const tempLicensePath = util.TmpDir + "/temp_vertica_license"

type VUpgradeLicenseOptions struct {
	DatabaseOptions

	// Required argument
	LicenseFilePath string

	// Optional argument, if not provided, then assume license file on localhost
	LicenseHost string
	// Calculated hidden argument, any value passed from callers will be ignored
	LocalHostAddr string
	// hidden argument for early check on file type, any value passed from callers will be ignored
	StageLicensePath string
}

func VUpgradeLicenseFactory() VUpgradeLicenseOptions {
	options := VUpgradeLicenseOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VUpgradeLicenseOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VUpgradeLicenseOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(UpgradeLicenseCmd, logger)
	if err != nil {
		return err
	}
	if options.LicenseFilePath == "" {
		return fmt.Errorf("must specify a license file")
	}
	if options.LicenseHost == "" {
		logger.Info("no license host provided, considering license file on local host")
	}
	// license file must be specified as an absolute path
	err = util.ValidateAbsPath(options.LicenseFilePath, "license file path")
	if err != nil {
		return err
	}
	return nil
}

func (options *VUpgradeLicenseOptions) validateParseOptions(log vlog.Printer) error {
	// validate required parameters
	err := options.validateRequiredOptions(log)
	if err != nil {
		return err
	}

	err = options.validateAuthOptions(UpgradeLicenseCmd.CmdString(), log)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VUpgradeLicenseOptions) analyzeOptions() (err error) {
	// make sure the specified file path has a file extension
	licenseExt := filepath.Ext(options.LicenseFilePath)
	// no extension, it's not a file
	if licenseExt == "" {
		return fmt.Errorf("must specify a valid file path")
	}

	if options.LicenseHost != "" {
		// resolve license host to be IP addresses
		licenseHostAddr, err := util.ResolveToOneIP(options.LicenseHost, options.IPv6)
		if err != nil {
			return err
		}

		options.LicenseHost = licenseHostAddr
	} else {
		// using localhost as source host for transferring the license file, should resolve localhost to one IP
		// resolve license host to be IP addresses
		localHostAddr, err := util.ResolveToOneIP(util.LocalHost, options.IPv6)
		if err != nil {
			return err
		}
		options.LocalHostAddr = localHostAddr
		// also set the temporary license file path for staging the file on a remote host
		options.StageLicensePath = tempLicensePath + licenseExt
	}
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}
	return nil
}

func (options *VUpgradeLicenseOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := options.validateParseOptions(log); err != nil {
		return err
	}
	if err := options.analyzeOptions(); err != nil {
		return err
	}
	if err := options.setUsePassword(log); err != nil {
		return err
	}
	return options.validateUserName(log)
}

func (vcc VClusterCommands) VUpgradeLicense(options *VUpgradeLicenseOptions) error {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// produce create acchive instructions
	instructions, err := vcc.produceUpgradeLicenseInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce INSTRUCTIONS, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to upgrade license: %w", runError)
	}
	return nil
}

// The generated instructions will later perform the following operations necessary
// if users specify a remote license host:
//   - Run install license API
//
// otherwise:
//   - Transfer the specified license file on localhost to an UP node
//   - Run install license API on the UP primary node
//   - Delete the temporary license file on the UP primary node
func (vcc *VClusterCommands) produceUpgradeLicenseInstructions(options *VUpgradeLicenseOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	vdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return instructions, err
	}

	// get up hosts
	hosts := options.Hosts
	// Trim host list
	hosts = vdb.filterUpHostListBySandbox(hosts, util.MainClusterSandbox)

	// should not happen, but adding a guardrail
	if len(hosts) == 0 {
		return instructions, fmt.Errorf("found no UP nodes for upgrading license")
	}

	// shortcut if users specified a remote host
	if options.LicenseHost != "" {
		// if specified license host isn't an UP host, error out
		// this license upgrade has to be done in main cluster
		if !util.StringInArray(options.LicenseHost, hosts) {
			return instructions, fmt.Errorf("license file must be on an UP host, the specified host %s is not UP", options.LicenseHost)
		}

		initiatorHost := []string{options.LicenseHost}

		httpsInstallLicenseOp, err := makeHTTPSInstallLicenseOp(initiatorHost, options.usePassword,
			options.UserName, options.Password, options.LicenseFilePath)
		if err != nil {
			return instructions, err
		}

		instructions = append(instructions, &httpsInstallLicenseOp)
		return instructions, nil
	}
	// if users do not specify a remote license host, transfer local license file to a primary UP node and perform upgrade
	// initiator is the host on which HTTPS install license endpoint will run
	initiator, setInitiatorErr := getInitiatorHost(vdb.PrimaryUpNodes, []string{} /* skip hosts */)
	if setInitiatorErr != nil {
		return instructions, setInitiatorErr
	}

	// step 1: transfer the local license file to the initiator host
	produceTransferLicenseOps(&instructions, options.LocalHostAddr, initiator,
		options.LicenseFilePath, options.StageLicensePath)

	initiatorHost := []string{initiator}
	// step 2: upgrade license
	httpsInstallLicenseOp, makeOpErr := makeHTTPSInstallLicenseOp(initiatorHost, options.usePassword,
		options.UserName, options.Password, options.StageLicensePath)
	if makeOpErr != nil {
		return instructions, makeOpErr
	}
	instructions = append(instructions, &httpsInstallLicenseOp)

	// step 3: clean up the stage license file
	nmaDeleteFileOp := makeNMADeleteFileOp(initiatorHost, options.StageLicensePath)
	instructions = append(instructions, &nmaDeleteFileOp)
	return instructions, nil
}
