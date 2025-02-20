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

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaPollCertHealthOp struct {
	opBase
	// while processing results, store responsive hosts for later reporting
	okHosts []string
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
		// show the hosts that aren't responding
		msg := fmt.Sprintf("did not successfully poll for NMA certificate health on the hosts '%v', details: %s",
			util.SliceDiff(op.hosts, op.okHosts), err)
		op.logger.PrintError(msg)
		return errors.New(msg)
	}

	return nil
}

func (op *nmaPollCertHealthOp) getPollingTimeout() int {
	const sixMinutes = OneMinute * 6 // make the linter happy
	return sixMinutes
}

func (op *nmaPollCertHealthOp) shouldStopPolling() (bool, error) {
	var allErrs error
	op.okHosts = []string{} // reset this to avoid removing hosts that succeed, then fail

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		// if good, add to okHosts
		op.logResponse(host, result)

		// expected if NMA can't verify the new client certs yet because
		// it's using the old trusted CA
		if result.isUnauthorizedRequest() {
			op.logger.Info("NMA reports unauthorized request, continuing to poll", "host", host)
			continue
		}

		if !result.isPassing() {
			// a failure result other than 401 from the NMA after connection succeeds is unexpected
			if result.isFailing() {
				allErrs = errors.Join(allErrs, result.err)
				continue
			}
			// expected if vclusterops validation of NMA cert is enabled and
			// the NMA isn't using its new certs yet
			if result.isException() {
				op.logger.Info("Possible TLS exception while attempting to connect to NMA", "host", host, "error", result.err.Error())
				continue
			}
			// if the NMA is shut down properly, it should drain connections before restarting, which means
			// this should not happen nearly as frequently as with the HTTPS service or indeed at all, but
			// there's no reason not to be safe and consider this a retry condition
			if result.isEOF() {
				op.logger.Info("Possible connection reset due to NMA restart", "host", host, "error", result.err.Error())
				continue
			}
			// EoF, exception, and http return code != 200 are the only usual non-passing cases, so this is something unknown.
			// To be safe, keep retrying until the result changes or the op times out.
			op.logger.Info("Unknown non-passing result while attempting to connect to the HTTPS service", "host", host,
				"error", result.err.Error())
			continue
		}

		// sanity check response
		_, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
		} else {
			op.okHosts = append(op.okHosts, host)
		}
	}

	// return immediately if there are unexpected failures
	if allErrs != nil {
		return true, allErrs
	}

	// check to see if we've heard from all the hosts yet
	healthyNodeCount := len(op.okHosts)
	if healthyNodeCount < len(op.hosts) {
		op.logger.PrintInfo("[%s] %d host(s) succeeded with handshake", op.name, healthyNodeCount)
		op.updateSpinnerMessage("%d host(s) succeeded with handshake, expecting %d host(s)", healthyNodeCount, len(op.hosts))
		return false, nil
	}

	op.logger.PrintInfo("[%s] All nodes succeeded with handshake", op.name)
	op.updateSpinnerStopMessage("all nodes succeeded with handshake")

	return true, nil
}
