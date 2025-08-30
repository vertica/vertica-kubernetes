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
	// If provided, these cert fields will be used for polling and the cert feilds in DatabaseOptions will be used
	// for reading tls digest. That is mainly for cert rotation. For other scenarios, the same cert can be used for all steps.
	NewKey    string
	NewCert   string
	NewCaCert string
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
	var instructionForReadingDigest []clusterOp
	instructionForReadingDigest = append(instructionForReadingDigest, &nmaGetTLSConfigDigestOp)
	databaseOptionsForReadingDigest := options.DatabaseOptions
	databaseOptionsForReadingDigest.Key = options.Key
	databaseOptionsForReadingDigest.Cert = options.Cert
	databaseOptionsForReadingDigest.CaCert = options.CaCert
	clusterOpEngineForReadingDigest := makeClusterOpEngine(instructionForReadingDigest, &databaseOptionsForReadingDigest)
	vcc.Log.Info("Retrieving updated TLS configuration digest")
	runErrorFromReadingDigest := clusterOpEngineForReadingDigest.run(vcc.Log)
	if runErrorFromReadingDigest != nil {
		return fmt.Errorf("failed to retrieve updated TLS configuration digest: %w", runErrorFromReadingDigest)
	}
	var instructionForPollAndSync []clusterOp
	instructionForPollAndSync = append(instructionForPollAndSync, &httpsPollCertHealthOp)
	if options.SyncCatalogRequired {
		httpsSyncCatalogOp, err2 := makeHTTPSSyncCatalogOp(mainClusterHosts, true, options.UserName, options.Password, CreateDBSyncCat)
		if err2 != nil {
			return err2
		}
		instructionForPollAndSync = append(instructionForPollAndSync, &httpsSyncCatalogOp)
	}
	databaseOptionsForPollAndSync := options.DatabaseOptions
	databaseOptionsForPollAndSync.Key = options.NewKey
	databaseOptionsForPollAndSync.Cert = options.NewCert
	databaseOptionsForPollAndSync.CaCert = options.NewCaCert
	clusterOpEngineForPollAndSync := makeClusterOpEngine(instructionForPollAndSync, &databaseOptionsForPollAndSync)
	if options.SyncCatalogRequired {
		vcc.Log.Info("Polling for HTTPS service restart on all UP hosts with updated catalog", "hosts", options.Hosts)
	} else {
		vcc.Log.Info("Polling for HTTPS service restart on all UP hosts", "hosts", options.Hosts)
	}
	runErrorFromPollAndSync := clusterOpEngineForPollAndSync.run(vcc.Log)
	if runErrorFromPollAndSync != nil {
		return fmt.Errorf("failed to restart HTTPS service with new tls version or cipher suites: %w", runErrorFromPollAndSync)
	}
	vcc.Log.Info("Polling for HTTPS service succeeded.", "new digest", expectedTLSConfigInfo.Digest)
	return nil
}
