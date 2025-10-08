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

type VCheckLicenseOptions struct {
	DatabaseOptions
	LicenseFile         string
	CELicenseDisallowed bool
}

func VCheckLicenseOptionsFactory() VCheckLicenseOptions {
	opt := VCheckLicenseOptions{}
	// set default values to the params
	opt.setDefaultValues()
	return opt
}

func (opt *VCheckLicenseOptions) analyzeOptions() (err error) {
	if len(opt.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		opt.Hosts, err = util.ResolveRawHostsToAddresses(opt.RawHosts, opt.IPv6)
		if err != nil {
			return err
		}
		opt.normalizePaths()
	}
	if opt.LicenseFile == "" {
		return fmt.Errorf("LicenseFile field of VCheckLicenseOptions cannot be empty")
	}
	return nil
}

func (opt *VCheckLicenseOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := opt.analyzeOptions(); err != nil {
		return err
	}
	if err := opt.setUsePasswordAndValidateUsernameIfNeeded(log); err != nil {
		return err
	}
	log.Info("Certificate authentication for HTTPS ops", "isEnabled", !opt.usePassword)
	return nil
}

func (vcc VClusterCommands) VCheckLicense(options *VCheckLicenseOptions) error {
	// validate and analyze all options
	optError := options.validateAnalyzeOptions(vcc.Log)
	if optError != nil {
		return optError
	}
	nmaCheckLicenseOp, err := makeNMACheckLicenseOp(options.Hosts, options.UserName, options.DBName, options.LicenseFile,
		options.Password, options.usePassword, options.CELicenseDisallowed, vcc.Log)
	if err != nil {
		return err
	}
	// As this endpoint is only used by k8s, nma_vertica_version_op is not required.
	// If it is to be used by vcluster cli, nma_vertica_version_op should be added.
	var instructions []clusterOp
	nmaHealthOp := makeNMAHealthOp(options.Hosts)
	instructions = append(instructions,
		&nmaHealthOp,
		&nmaCheckLicenseOp,
	)
	clusterOpEngine := makeClusterOpEngine(instructions, options)
	vcc.Log.Info("Checking Vertica License ", "hosts", options.Hosts)
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("failed to check Vertica License: %w", runError)
	}
	return nil
}
