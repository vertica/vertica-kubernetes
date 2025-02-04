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
	"golang.org/x/exp/slices"
)

type VCheckVClusterServerPidOptions struct {
	DatabaseOptions
}

func VCheckVClusterServerPidOptionsFactory() VCheckVClusterServerPidOptions {
	options := VCheckVClusterServerPidOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VCheckVClusterServerPidOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VCheckVClusterServerPidOptions) analyzeOptions() (err error) {
	// resolve RawHosts to be IP addresses
	if len(options.RawHosts) > 0 {
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VCheckVClusterServerPidOptions) validateAnalyzeOptions(_ vlog.Printer) error {
	err := options.analyzeOptions()
	if err != nil {
		return err
	}
	return nil
}

func (vcc VClusterCommands) VCheckVClusterServerPid(
	options *VCheckVClusterServerPidOptions) (hostsWithVclusterServerPid []string, err error) {
	// validate and analyze options
	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return hostsWithVclusterServerPid, err
	}

	// produce instructions of checking VCluster server PID files
	instructions, err := vcc.produceCheckVClusterServerPidInstructions(options)
	if err != nil {
		return hostsWithVclusterServerPid, fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return hostsWithVclusterServerPid, fmt.Errorf("fail to check VCluster server PID files: %w", runError)
	}

	hostsWithVclusterServerPid = clusterOpEngine.execContext.HostsWithVclusterServerPid
	slices.Sort(hostsWithVclusterServerPid)

	return hostsWithVclusterServerPid, nil
}

func (vcc VClusterCommands) produceCheckVClusterServerPidInstructions(options *VCheckVClusterServerPidOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	nmaHealthOp := makeNMAHealthOpSkipUnreachable(options.Hosts)

	nmaCheckVClusterServerPidOp := makeNMACheckVClusterServerPidOp(options.Hosts)

	instructions = append(instructions, &nmaHealthOp, &nmaCheckVClusterServerPidOp)
	return instructions, nil
}
