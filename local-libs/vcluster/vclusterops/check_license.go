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
	LicenseFile string
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

func (vcc VClusterCommands) VCheckLicense(options *VCheckLicenseOptions) (CheckLicenseResponse, error) {
	// validate and analyze all options
	optError := options.validateAnalyzeOptions(vcc.Log)
	if optError != nil {
		return nil, optError
	}
	checkLicenseResponse := CheckLicenseResponse{}
	nmaCheckLicenseOp, err := makeNMACheckLicenseOp(options.Hosts, options.UserName, options.DBName, options.LicenseFile,
		options.Password, options.usePassword, checkLicenseResponse, vcc.Log)
	if err != nil {
		return nil, err
	}

	var instructions []clusterOp
	instructions = append(instructions, &nmaCheckLicenseOp)
	clusterOpEngine := makeClusterOpEngine(instructions, options)
	vcc.Log.Info("Checking Vertica License ", "hosts", options.Hosts)
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return nil, fmt.Errorf("failed to check Vertica License: %w", runError)
	}
	vcc.Log.Info("Checking Vertica License succeeded.", "response", checkLicenseResponse)
	return checkLicenseResponse, nil
}
