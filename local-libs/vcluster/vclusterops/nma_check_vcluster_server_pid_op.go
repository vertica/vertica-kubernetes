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
	"errors"
)

// we limit the check timeout to 30 seconds
// we believe that this is enough to test the NMA connection
const nmaCheckVClusterServerPidTimeout = 30

type nmaCheckVClusterServerPidOp struct {
	opBase
}

func makeNMACheckVClusterServerPidOp(hosts []string) nmaCheckVClusterServerPidOp {
	op := nmaCheckVClusterServerPidOp{}
	op.name = "NMACheckVClusterServerPidOp"
	op.description = "Check whether the VCluster server PID file exists"
	op.hosts = hosts
	return op
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaCheckVClusterServerPidOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("health/vcluster-server")
		httpRequest.Timeout = nmaCheckVClusterServerPidTimeout
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaCheckVClusterServerPidOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaCheckVClusterServerPidOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaCheckVClusterServerPidOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaCheckVClusterServerPidOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			resultMap, err := op.parseAndCheckMapResponse(host, result.content)
			if err != nil {
				return errors.Join(allErrs, err)
			}
			exist, ok := resultMap["vcluster_server_pid_file_exists"]
			if !ok {
				e := errors.New(`the key "vcluster_server_pid_file_exists" does not exist in the response`)
				allErrs = errors.Join(allErrs, e)
			}
			if exist == "true" {
				execContext.HostsWithVclusterServerPid = append(execContext.HostsWithVclusterServerPid, host)
			}
		}
	}

	return allErrs
}
