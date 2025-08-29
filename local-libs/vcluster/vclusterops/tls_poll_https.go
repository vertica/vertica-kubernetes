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

type VPollHTTPSOptions struct {
	DatabaseOptions
	MainClusterInitiator string
	SyncCatalogRequired  bool
	NewKey               string
	NewCert              string
	NewCaCert            string
}

func VPollHTTPSOptionsFactory() VPollHTTPSOptions {
	opt := VPollHTTPSOptions{}
	// set default values to the params
	opt.setDefaultValues()
	return opt
}

func (opt *VPollHTTPSOptions) analyzeOptions() (err error) {
	if len(opt.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		opt.Hosts, err = util.ResolveRawHostsToAddresses(opt.RawHosts, opt.IPv6)
		if err != nil {
			return err
		}
		opt.normalizePaths()
	}
	if opt.NewKey == "" || opt.NewCert == "" || opt.NewCaCert == "" {
		err = fmt.Errorf("new cert has empty field")
	}
	return err
}

func (opt *VPollHTTPSOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := opt.analyzeOptions(); err != nil {
		return err
	}
	if err := opt.setUsePasswordAndValidateUsernameIfNeeded(log); err != nil {
		return err
	}
	log.Info("Certificate authentication for HTTPS ops", "isEnabled", !opt.usePassword)
	return nil
}

func (vcc VClusterCommands) VPollHTTPS(options *VPollHTTPSOptions) error {
	// validate and analyze all options
	optError := options.validateAnalyzeOptions(vcc.Log)
	if optError != nil {
		return optError
	}
	mainClusterHosts := strings.Split(options.MainClusterInitiator, ",")
	upHosts := options.Hosts
	expectedTLSConfigInfo := &tlsConfigInfo{
		Digest:      "",
		IsBootstrap: false,
	}
	nmaGetTLSConfigDigestOp, err := makeNMAGetTLSConfigDigestOp(mainClusterHosts,
		options.UserName, options.DBName, "https", options.Password, options.usePassword, expectedTLSConfigInfo, vcc.Log)
	if err != nil {
		return err
	}
	httpsPollCertHealthOp, err := makeHTTPSPollCertificateHealthOp(upHosts,
		expectedTLSConfigInfo, options.usePassword, options.UserName, options.Password)
	if err != nil {
		return err
	}
	var instructionOne []clusterOp
	instructionOne = append(instructionOne, &nmaGetTLSConfigDigestOp)
	databaseOptionsOne := options.DatabaseOptions
	databaseOptionsOne.Key = options.Key
	databaseOptionsOne.Cert = options.Cert
	databaseOptionsOne.CaCert = options.CaCert
	clusterOpEngineOne := makeClusterOpEngine(instructionOne, &databaseOptionsOne)
	vcc.Log.Info("Retrieving updated TLS configuration digest")
	runErrorOne := clusterOpEngineOne.run(vcc.Log)
	if runErrorOne != nil {
		return fmt.Errorf("failed to retrieve updated TLS configuration digest: %w", runErrorOne)
	}
	var instructionTwo []clusterOp
	instructionTwo = append(instructionTwo, &httpsPollCertHealthOp)
	if options.SyncCatalogRequired {
		httpsSyncCatalogOp, err2 := makeHTTPSSyncCatalogOp(mainClusterHosts, true, options.UserName, options.Password, CreateDBSyncCat)
		if err2 != nil {
			return err2
		}
		instructionTwo = append(instructionTwo, &httpsSyncCatalogOp)
	}
	databaseOptionsTwo := options.DatabaseOptions
	databaseOptionsTwo.Key = options.NewKey
	databaseOptionsTwo.Cert = options.NewCert
	databaseOptionsTwo.CaCert = options.NewCaCert
	clusterOpEngineTwo := makeClusterOpEngine(instructionTwo, &databaseOptionsTwo)
	if options.SyncCatalogRequired {
		vcc.Log.Info("Polling for HTTPS service restart on all UP hosts with updated catalog", "hosts", options.Hosts)
	} else {
		vcc.Log.Info("Polling for HTTPS service restart on all UP hosts", "hosts", options.Hosts)
	}
	runErrorTwo := clusterOpEngineTwo.run(vcc.Log)
	if runErrorTwo != nil {
		return fmt.Errorf("failed to restart HTTPS service with new tls version or cipher suites: %w", runErrorTwo)
	}
	vcc.Log.Info("Polling for HTTPS service succeeded.", "new digest", expectedTLSConfigInfo.Digest)
	return nil
}
