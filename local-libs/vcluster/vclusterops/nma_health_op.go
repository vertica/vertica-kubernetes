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

// we limit the health check timeout to 30 seconds
// we believe that this is enough to test the NMA connection
const nmaHealthCheckTimeout = 30

type nmaHealthOp struct {
	opBase
	// sometimes, we need to skip unreachable hosts
	// e.g., list_all_nodes may need this when the host(s) are not connectable
	skipUnreachableHost bool
}

func makeNMAHealthOp(hosts []string) nmaHealthOp {
	op := nmaHealthOp{}
	op.name = "NMAHealthOp"
	op.description = "Check NMA service health"
	op.hosts = hosts
	return op
}

func makeNMAHealthOpSkipUnreachable(hosts []string) nmaHealthOp {
	op := makeNMAHealthOp(hosts)
	op.skipUnreachableHost = true
	return op
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaHealthOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("health")
		httpRequest.Timeout = nmaHealthCheckTimeout
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaHealthOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaHealthOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaHealthOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaHealthOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	var unreachableHosts []string
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			_, err := op.parseAndCheckMapResponse(host, result.content)
			if err != nil {
				return errors.Join(allErrs, err)
			}
		} else {
			unreachableHosts = append(unreachableHosts, host)
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	if op.skipUnreachableHost {
		execContext.unreachableHosts = unreachableHosts
		if len(unreachableHosts) > 0 {
			op.stopFailSpinnerWithMessage("warning! hosts %v are unreachable", unreachableHosts)
		}
		return nil
	}

	return allErrs
}
