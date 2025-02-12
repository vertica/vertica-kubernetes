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

type VRotateNMACertsOptions struct {
	/* Part 1: basic DB info */
	DatabaseOptions

	/*
	 * Part 2: new TLS configuration options
	 * If the NMA has already been restarted (controlled by the flag below)
	 * then only these certs will be used and not the DatabaseOptions ones.
	 */
	// TLS Key
	NewKey string
	// TLS Certificate
	NewCert string
	// TLS CA Certificate
	NewCaCert string

	// whether the NMA needs to be shut down before polling for changes
	DoKillNMA bool
}

func VRotateNMACertsOptionsFactory() VGetConfigurationParameterOptions {
	opt := VGetConfigurationParameterOptions{}
	// set default values to the params
	opt.setDefaultValues()

	return opt
}

func (opt *VRotateNMACertsOptions) validateParseOptions(logger vlog.Printer) error {
	return opt.validateBaseOptions(RotateNMACertsCmd, logger)
}

func (opt *VRotateNMACertsOptions) analyzeOptions() (err error) {
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

func (opt *VRotateNMACertsOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := opt.validateParseOptions(log); err != nil {
		return err
	}
	if err := opt.analyzeOptions(); err != nil {
		return err
	}
	// only using NMA ops in the initial implementation, so username/pw are
	// irrelevant, but this doesn't hurt
	return opt.setUsePasswordAndValidateUsernameIfNeeded(log)
}

// VRotateNMACerts polls for NMA restart using new certificates.  It may kill the NMA
// first using the old certs, if requested.
// It returns any error encountered.
func (vcc VClusterCommands) VRotateNMACerts(options *VRotateNMACertsOptions) error {
	// validate and analyze all options
	optError := options.validateAnalyzeOptions(vcc.Log)
	if optError != nil {
		return optError
	}

	// produce optional NMA-killing instructions
	if options.DoKillNMA {
		instructions, err := vcc.produceKillNMAInstructions(options)
		if err != nil {
			return fmt.Errorf("failed to produce kill NMA instructions, %w", err)
		}

		// Create a VClusterOpEngine, and add certs to the engine
		clusterOpEngine := makeClusterOpEngine(instructions, options)

		// Give the instructions to the VClusterOpEngine to run
		runError := clusterOpEngine.run(vcc.Log)
		if runError != nil {
			return fmt.Errorf("failed to kill the NMA: %w", runError)
		}
	}

	// produce polling instructions
	instructions, err := vcc.produceRotateNMACertsInstructions(options)
	if err != nil {
		return fmt.Errorf("failed to produce rotate NMA certs instructions, %w", err)
	}

	// create db options with only cert info changed
	newCertsDatabaseOptions := options.DatabaseOptions
	newCertsDatabaseOptions.Key = options.NewKey
	newCertsDatabaseOptions.Cert = options.NewCert
	newCertsDatabaseOptions.CaCert = options.NewCaCert

	// Create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, &newCertsDatabaseOptions)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("failed to rotate NMA certs: %w", runError)
	}

	return nil
}

// The generated instructions will later perform the following operations necessary
// to kill the NMA prior to polling with new certificates
//   - Check NMA connectivity
//   - Send shutdown NMA request
func (vcc VClusterCommands) produceKillNMAInstructions(
	options *VRotateNMACertsOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	nmaHealthOp := makeNMAHealthOp(options.Hosts)
	nmaShutdownOp := makeNMAShutdownOp(options.Hosts)
	instructions = append(instructions,
		&nmaHealthOp,
		&nmaShutdownOp,
	)
	return instructions, nil
}

// The generated instructions will later perform the following operations necessary
// to check if the NMA certs have been rotated
//   - Check NMA connectivity (with new certs)
func (vcc VClusterCommands) produceRotateNMACertsInstructions(
	options *VRotateNMACertsOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	nmaPollCertHealthOp := makeNMAPollCertHealthOp(options.Hosts)
	instructions = append(instructions, &nmaPollCertHealthOp)
	return instructions, nil
}
