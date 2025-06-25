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

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type VSetTLSConfigOptions struct {
	DatabaseOptions
	// Server TLS Configuration
	ServerTLSConfig TLSConfig
	// HTTPS TLS Configuration
	HTTPSTLSConfig TLSConfig
}

const DefaultCacheDuration = 0

func VSetTLSConfigOptionsFactory() VSetTLSConfigOptions {
	options := VSetTLSConfigOptions{}
	options.setDefaultValues()
	options.ServerTLSConfig = TLSConfig{
		ConfigMap:     make(map[string]string),
		ConfigType:    ServerTLSKeyPrefix,
		GrantAuth:     false,
		CacheDuration: uint64(DefaultCacheDuration),
	}
	options.HTTPSTLSConfig = TLSConfig{
		ConfigMap:     make(map[string]string),
		ConfigType:    HTTPSTLSKeyPrefix,
		GrantAuth:     true,
		CacheDuration: uint64(DefaultCacheDuration),
	}

	return options
}

// validateTLSConfig makes sure the tls configuration for server and/or https
// has the required fields with appropriate values
func (options *VSetTLSConfigOptions) validateTLSConfig(logger vlog.Printer) error {
	var err error

	if !options.ServerTLSConfig.hasConfigParam() && !options.HTTPSTLSConfig.hasConfigParam() {
		return fmt.Errorf("missing TLS configuration: specify settings for at least one of server or HTTPS")
	}

	if options.ServerTLSConfig.GrantAuth == options.HTTPSTLSConfig.GrantAuth {
		return fmt.Errorf("server and https TLS configurations cannot both set GrantAuth to true or both set to false")
	}

	err = options.ServerTLSConfig.validate(logger)
	if err != nil {
		return err
	}

	return options.HTTPSTLSConfig.validate(logger)
}

func (options *VSetTLSConfigOptions) analyzeOptions() (err error) {
	return options.resolveToIPAndNormalizePaths()
}

func (options *VSetTLSConfigOptions) validateParseOptions(logger vlog.Printer) error {
	// validate base options
	err := options.validateBaseOptions(SetTLSConfigCmd, logger)
	if err != nil {
		return err
	}

	return options.validateTLSConfig(logger)
}

func (options *VSetTLSConfigOptions) validateAnalyzeOptions(log vlog.Printer) error {
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

func (vcc VClusterCommands) VSetTLSConfig(options *VSetTLSConfigOptions) error {
	// validate and analyze all options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	instructions, err := vcc.produceSetTLSConfigInstructions(options)
	if err != nil {
		return err
	}

	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// Give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to set tls config: %w", runError)
	}

	return nil
}

func (vcc VClusterCommands) produceSetTLSConfigInstructions(options *VSetTLSConfigOptions) ([]clusterOp, error) {
	var instructions []clusterOp
	nmaHealthOp := makeNMAHealthOp(options.Hosts)
	instructions = append(instructions, &nmaHealthOp)
	if options.ServerTLSConfig.hasConfigParam() {
		nmaSetServerTLSOp, err := makeNMASetTLSOp(&options.DatabaseOptions, string(options.ServerTLSConfig.ConfigType),
			options.ServerTLSConfig.GrantAuth,
			true, // syncCatalog
			options.ServerTLSConfig.CacheDuration,
			options.ServerTLSConfig.ConfigMap)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &nmaSetServerTLSOp)
	}

	if options.HTTPSTLSConfig.hasConfigParam() {
		nmaSetHTTPSTLSOp, err := makeNMASetTLSOp(&options.DatabaseOptions, string(options.HTTPSTLSConfig.ConfigType),
			options.HTTPSTLSConfig.GrantAuth,
			true, // syncCatalog
			options.HTTPSTLSConfig.CacheDuration,
			options.HTTPSTLSConfig.ConfigMap)
		if err != nil {
			return instructions, err
		}
		instructions = append(instructions, &nmaSetHTTPSTLSOp)
	}

	return instructions, nil
}
