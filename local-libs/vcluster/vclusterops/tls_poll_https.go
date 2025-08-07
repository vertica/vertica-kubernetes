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

type VPollHTTPSOptions struct {
	/*
	 * Part 1: basic DB info
	 * Note that unlike every other vclusterops command out there,
	 * specifying the password will only use it for NMA SQL operations,
	 * not HTTPS service operations, unless AllowPasswordAuthForHTTPSOps
	 * is also set to true.
	 */
	DatabaseOptions
	TLSVersion       int
	TLSConfigDigest  string
	MainClusterHosts []string
}

func VPollHTTPSOptionsFactory() VPollHTTPSOptions {
	opt := VPollHTTPSOptions{}
	// set default values to the params
	opt.setDefaultValues()
	return opt
}

func (opt *VPollHTTPSOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
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

func (opt *VPollHTTPSOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := opt.analyzeOptions(); err != nil {
		return err
	}
	if opt.TLSVersion != 2 && opt.TLSVersion != 3 {
		return fmt.Errorf("invalid tls version - %d", opt.TLSVersion)
	}
	if opt.TLSConfigDigest == "" {
		return fmt.Errorf("tls config digest cannot be empty")
	}
	// NMA -> Vertica cert auth is finicky.  If it isn't set up right, we still need
	// username/pw for the NMA to authenticate to Vertica, even if cert auth works
	// for the HTTPS service.
	if err := opt.setUsePasswordAndValidateUsernameIfNeeded(log); err != nil {
		return err
	}
	opt.usePassword = false
	log.Info("Certificate authentication for HTTPS ops", "isEnabled", !opt.usePassword)
	return nil
}

func (vcc VClusterCommands) VPollHTTPS(options *VPollHTTPSOptions) error {
	// validate and analyze all options
	optError := options.validateAnalyzeOptions(vcc.Log)
	if optError != nil {
		return optError
	}

	/* vdb := makeVCoordinationDatabase()
	err := vcc.getDeepVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return err
	}

	// the rotation operations need one UP host from each sandbox + main cluster.  the
	// polling operations should poll each previously UP host in the entire cluster
	// for restart.
	upHosts, mainClusterHosts, err := options.getHostInfo(&vdb)
	if err != nil {
		return err
	} */

	mainClusterHosts := options.MainClusterHosts
	upHosts := options.Hosts

	// If we're rotating the https service config, cache the fingerprint of the updated
	// tls config so we can poll for restart.
	// Polling for other tls config updates is NYI, but error scenarios are much less likely.
	expectedTLSConfigInfo := &tlsConfigInfo{
		Digest:      "",
		IsBootstrap: false,
	}

	nmaGetTLSConfigDigestOp, err := makeNMAGetTLSConfigDigestOp(mainClusterHosts,
		options.UserName, options.DBName, "https", options.Password, options.usePassword, expectedTLSConfigInfo, vcc.Log)

	httpsPollCertHealthOp, err := makeHTTPSPollCertificateHealthOp(upHosts,
		expectedTLSConfigInfo, options.usePassword, options.UserName, options.Password)
	if err != nil {
		return err
	}
	var instructions []clusterOp
	instructions = append(instructions, &nmaGetTLSConfigDigestOp)
	instructions = append(instructions, &httpsPollCertHealthOp)
	httpsSyncCatalogOp, err2 := makeHTTPSSyncCatalogOp(mainClusterHosts, true, options.UserName, options.Password, CreateDBSyncCat)
	if err2 != nil {
		return err2
	}
	instructions = append(instructions, &httpsSyncCatalogOp)

	// create db options with only cert info changed
	newCertsDatabaseOptions := options.DatabaseOptions
	newCertsDatabaseOptions.Key = options.Key
	newCertsDatabaseOptions.Cert = options.Cert
	newCertsDatabaseOptions.CaCert = options.CaCert

	// Create a VClusterOpEngine with the new certs
	clusterOpEngine := makeClusterOpEngine(instructions, &newCertsDatabaseOptions)

	// Give the instructions to the VClusterOpEngine to run
	vcc.Log.Info("Polling for HTTPS service restart with updated config on all UP hosts", "hosts", options.Hosts)
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("failed to restart HTTPS service with new tls version or cipher suites: %w", runError)
	}
	vcc.Log.Info("libo: new digest - " + expectedTLSConfigInfo.Digest + ", old digest - " + options.TLSConfigDigest)

	return nil
}

func (opt *VPollHTTPSOptions) getHostInfo(
	vdb *VCoordinationDatabase) (upHosts, mainClusterHosts []string, err error) {
	upHosts = vdb.filterUpHostList(opt.Hosts)
	// avoid mutating backing array of vdb.AllSandboxes
	sandboxes := make([]string, len(vdb.AllSandboxes), len(vdb.AllSandboxes)+1)
	copy(sandboxes, vdb.AllSandboxes)
	sandboxes = append(sandboxes, "") // add main cluster to sandbox list
	mainCluster := []string{""}
	hostsToSandboxes := vdb.getHostToSandboxMap()
	mainClusterHosts, err = getInitiatorsInAllDBGroups(upHosts, mainCluster, hostsToSandboxes)
	if len(mainCluster) == 0 {
		err = fmt.Errorf("failed to find an initiator host for main cluster")
	}
	return
}
