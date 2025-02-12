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
)

type nmaPollCertHealthOp struct {
	opBase
}

func makeNMAPollCertHealthOp(hosts []string) nmaPollCertHealthOp {
	op := nmaPollCertHealthOp{}
	op.name = "NMAPollCertHealthOp"
	op.description = "Check NMA service certificate health"
	op.hosts = hosts
	return op
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaPollCertHealthOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("health/certs")
		httpRequest.Timeout = nmaHealthCheckTimeout
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaPollCertHealthOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaPollCertHealthOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaPollCertHealthOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaPollCertHealthOp) processResult(execContext *opEngineExecContext) error {
	err := pollState(op, execContext)
	if err != nil {
		return fmt.Errorf("error polling NMA certificate health, %w", err)
	}

	return nil
}

func (op *nmaPollCertHealthOp) getPollingTimeout() int {
	const sixMinutes = OneMinute * 6 // make the linter happy
	return sixMinutes
}

func (op *nmaPollCertHealthOp) shouldStopPolling() (bool, error) {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		// expected if NMA can't verify the new client certs yet because
		// it's using the old trusted CA
		if result.isUnauthorizedRequest() {
			return false, nil
		}

		if !result.isPassing() {
			// expected if vclusterops validation of NMA cert is enabled and
			// the NMA isn't using its new certs yet
			if result.isException() {
				return false, nil
			}
			// anything else is a real error
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// sanity check response
		_, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
		}
	}

	return true, allErrs
}
