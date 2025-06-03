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
	"errors"
	"fmt"
	"time"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
	"golang.org/x/exp/slices"
)

type VRotateTLSCertsOptions struct {
	/*
	 * Part 1: basic DB info
	 * Note that unlike every other vclusterops command out there,
	 * specifying the password will only use it for NMA SQL operations,
	 * not HTTPS service operations, unless AllowPasswordAuthForHTTPSOps
	 * is also set to true.
	 */
	DatabaseOptions

	/*
	 * Part 2: new client TLS options
	 * Note that these are the PEM bytes of the new client key/cert pair and
	 * ca cert, used directly by vclusterops for polling after the HTTPS service
	 * TLS config is updated.  They are not the new HTTPS service key/cert pair,
	 * although the ca cert is probably the same.
	 * NMA operations here use the "old" client TLS params from the standard
	 * DatabaseOptions flags.
	 */
	NewClientTLSConfig

	/*
	 * Part 3: new TLS configuration options for the service
	 * These are used to alter the service TLS configuration
	 * and aren't used directly, just passed to the NMA.
	 */
	NewSecretMetadata RotateTLSCertsData

	/*
	 * Part 4: overriding this command's special username/pw behavior
	 * Due to the need to check if the server CA is updated, this API forces cert
	 * auth for HTTPS operations, even if the password is provided for the NMA to
	 * use for authentication with the database.
	 * Therefore, this flag should remain set to default (false) for the rotation
	 * polling to work properly unless the following is true:
	 * 1) It's necessary because certificate auth to the HTTPS service won't work,
	 *    e.g. TLS is set up for clients to validate the server, but the server
	 *    doesn't use TLS auth for clients
	 * 2) The new HTTPS service cert root signer is different than the old HTTPS
	 *    service cert root signer.
	 * 3) The appropriate new and old root ca certs are passed to this operation
	 *    (CaCert and NewCaCert).
	 * 4) Client (vclusterops) validation of the HTTPS service cert is enabled
	 *    (DoVerifyHTTPSServerCert == true).
	 */
	AllowPasswordAuthForHTTPSOps bool

	// The type of secret manager. It is one of "kubernetes", "AWS" and "GCP"
	TLSSecretManager string

	// internal use: controls NMA SQL op pw auth
	usePasswordForNMA bool
}

type RotateTLSCertsData struct {
	// name of the secret containing key data
	KeySecretName string `json:"key_secret_name"` // required
	// config used by the config manager to extract key data from secret
	KeyConfig string `json:"key_config,omitempty"`
	// name of the secret containing certificate data
	CertSecretName string `json:"cert_secret_name"` // required
	// config used by the config manager to extract cert data from secret
	CertConfig string `json:"cert_config,omitempty"`
	// name of the secret containing ca certificate data
	CACertSecretName string `json:"ca_cert_secret_name"` // required
	// config used by the config manager to extract ca cert data from secret
	CACertConfig string `json:"ca_cert_config,omitempty"`
	// if changing tls mode, vertica server tls mode, e.g. "verify_full"
	TLSMode string `json:"tlsmode,omitempty"`
	// tls config to rotate certs on. valid values are "HTTPS" or "Server"
	TLSConfig string `json:"tls_config,omitempty"` // required
}

func VRotateTLSCertsOptionsFactory() VRotateTLSCertsOptions {
	opt := VRotateTLSCertsOptions{}
	// set default values to the params
	opt.setDefaultValues()

	return opt
}

func (opt *VRotateTLSCertsOptions) validateParseOptions(logger vlog.Printer) error {
	if opt.NewSecretMetadata.KeySecretName == "" {
		return errors.New("KeySecretName cannot be empty")
	}
	if opt.NewSecretMetadata.CertSecretName == "" {
		return errors.New("CertSecretName cannot be empty")
	}
	if opt.NewSecretMetadata.CACertSecretName == "" {
		return errors.New("CACertSecretName cannot be empty")
	}
	validSecretManagerTypes := []string{K8sSecretManagerType, AWSSecretManagerType}
	if !slices.Contains(validSecretManagerTypes, opt.TLSSecretManager) {
		return fmt.Errorf("secretmanager type must be one of %s", validSecretManagerTypes)
	}

	return opt.validateBaseOptions(RotateVerticaCertsCmd, logger)
}

func (opt *VRotateTLSCertsOptions) analyzeOptions() (err error) {
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

func (opt *VRotateTLSCertsOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := opt.validateParseOptions(log); err != nil {
		return err
	}
	if err := opt.analyzeOptions(); err != nil {
		return err
	}
	// NMA -> Vertica cert auth is finicky.  If it isn't set up right, we still need
	// username/pw for the NMA to authenticate to Vertica, even if cert auth works
	// for the HTTPS service.
	if err := opt.setUsePasswordAndValidateUsernameIfNeeded(log); err != nil {
		return err
	}
	opt.usePasswordForNMA = opt.usePassword
	opt.usePassword = opt.usePassword && opt.AllowPasswordAuthForHTTPSOps
	log.Info("Certificate authentication for NMA SQL ops", "isEnabled", !opt.usePasswordForNMA)
	log.Info("Certificate authentication for HTTPS ops", "isEnabled", !opt.usePassword)
	return nil
}

// VRotateTLSCerts takes some parameters used by the secrets manager which Vertica
// hooks into for TLS configuration for the HTTPS service, and uses them to update
// that configuration, then polls for HTTPS service restart.
// It returns any error encountered.
func (vcc VClusterCommands) VRotateTLSCerts(options *VRotateTLSCertsOptions) error {
	// validate and analyze all options
	optError := options.validateAnalyzeOptions(vcc.Log)
	if optError != nil {
		return optError
	}

	// Construct a full vdb by enumerating main cluster node info and the sandbox list
	// from the main cluster, then updating sandbox node status from sandbox nodes.
	// Certs must be rotated across all sandboxes, so this operation will both retrieve
	// the necessary node status and sandbox information, and enforce that every sandbox
	vdb := makeVCoordinationDatabase()
	err := vcc.getDeepVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return err
	}

	// the rotation operations need one UP host from each sandbox + main cluster.  the
	// polling operations should poll each previously UP host in the entire cluster
	// for restart.
	upHosts, initiatorHosts, hostsToSandboxes, err := options.getVDBInfo(&vdb)
	if err != nil {
		return err
	}

	// produce rotation instructions
	instructions, err := vcc.produceRotateTLSCertsInstructions(options, initiatorHosts, hostsToSandboxes)
	if err != nil {
		return fmt.Errorf("failed to produce rotate HTTPS certs instructions, %w", err)
	}

	// Create a VClusterOpEngine with the old certs
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	vcc.Log.Info("Attempting to rotate the certs for the HTTPS service", "hosts", options.Hosts)
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("failed to rotate HTTPS service certs: %w", runError)
	}

	// produce polling instructions
	instructions, err = vcc.producePollHTTPSRestartInstructions(options, upHosts)
	if err != nil {
		return fmt.Errorf("failed to produce poll HTTPS restart instructions, %w", err)
	}

	// create db options with only cert info changed
	newCertsDatabaseOptions := options.DatabaseOptions
	newCertsDatabaseOptions.Key = options.NewKey
	newCertsDatabaseOptions.Cert = options.NewCert
	newCertsDatabaseOptions.CaCert = options.NewCaCert

	if options.NewSecretMetadata.TLSConfig == "Server" {
		time.Sleep(15 * OneSecond * time.Second)
		return nil
	}

	// Create a VClusterOpEngine with the new certs
	clusterOpEngine = makeClusterOpEngine(instructions, &newCertsDatabaseOptions)

	// Give the instructions to the VClusterOpEngine to run
	vcc.Log.Info("Polling for HTTPS service restart on all UP hosts", "hosts", options.Hosts)
	runError = clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("failed to restart HTTPS service with correct certs: %w", runError)
	}

	return nil
}

func (opt *VRotateTLSCertsOptions) getVDBInfo(
	vdb *VCoordinationDatabase) (upHosts, initiatorHosts []string, hostsToSandboxes map[string]string, err error) {
	upHosts = vdb.filterUpHostList(opt.Hosts)
	hostsToSandboxes = vdb.getHostToSandboxMap()
	// avoid mutating backing array of vdb.AllSandboxes
	sandboxes := make([]string, len(vdb.AllSandboxes), len(vdb.AllSandboxes)+1)
	copy(sandboxes, vdb.AllSandboxes)
	sandboxes = append(sandboxes, "") // add main cluster to sandbox list
	initiatorHosts, err = getInitiatorsInAllDBGroups(upHosts, sandboxes, hostsToSandboxes)
	return
}

// The generated instructions will later perform the following operations necessary
// to update the TLS config for the HTTPS service in all sandboxes and the main cluster.
//   - Check NMA connectivity
//   - Rotate the certs (and optionally update TLS mode)
func (vcc VClusterCommands) produceRotateTLSCertsInstructions(
	options *VRotateTLSCertsOptions,
	initiatorHosts []string,
	hostsToSandboxes map[string]string) ([]clusterOp, error) {
	var instructions []clusterOp
	nmaHealthOp := makeNMAHealthOp(initiatorHosts)
	nmaRotateTLSCertsOp, err := makeNMARotateTLSCertsOp(initiatorHosts, options.UserName,
		options.DBName, hostsToSandboxes, &options.NewSecretMetadata, options.TLSSecretManager,
		options.Password, options.usePasswordForNMA)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions,
		&nmaHealthOp,
		&nmaRotateTLSCertsOp,
	)
	return instructions, nil
}

// The generated instructions will later perform the following operations necessary
// to check if the HTTPS certs have been rotated
//   - Check HTTPS service connectivity (with new certs)
func (vcc VClusterCommands) producePollHTTPSRestartInstructions(
	options *VRotateTLSCertsOptions,
	upHosts []string) ([]clusterOp, error) {
	var instructions []clusterOp
	// the HTTPS service health endpoint requires a successful TLS handshake plus authentication
	httpsPollCertHealthOp, err := makeHTTPSPollCertificateHealthOp(upHosts,
		options.usePassword, options.UserName, options.Password)
	if err != nil {
		return instructions, err
	}
	instructions = append(instructions, &httpsPollCertHealthOp)
	return instructions, nil
}
