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

// httpsPollCertificateHealthOp polls the https service health endpoint, which unlike
// the NMA, requires authentication.  This op can be used with or without client
// certificates.  For example, server cert validation may be on despite using pw auth,
// and so changing the https service certificate may change connection behavior.
type httpsPollCertificateHealthOp struct {
	opBase
	opHTTPSBase
	// how long to continue polling, default 5 minutes (typical case <5 seconds)
	timeout int
	// while processing results, store responsive hosts for later reporting
	okHosts []string
}

func makeHTTPSPollCertificateHealthOp(hosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string) (httpsPollCertificateHealthOp, error) {
	op := httpsPollCertificateHealthOp{}
	op.name = "HTTPSPollCertificateHealthOp"
	op.description = "Wait for nodes to restart HTTPS service with new TLS config"
	op.hosts = hosts
	op.useHTTPPassword = useHTTPPassword
	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.userName = userName
	op.httpsPassword = httpsPassword
	op.timeout = util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", StartupPollingTimeout)
	return op, nil
}

func (op *httpsPollCertificateHealthOp) getPollingTimeout() int {
	return max(op.timeout, 0)
}

func (op *httpsPollCertificateHealthOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.Timeout = defaultHTTPSRequestTimeoutSeconds // 30s instead of 300s
		httpRequest.buildHTTPSEndpoint("health")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsPollCertificateHealthOp) prepare(execContext *opEngineExecContext) (err error) {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsPollCertificateHealthOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsPollCertificateHealthOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *httpsPollCertificateHealthOp) processResult(execContext *opEngineExecContext) error {
	op.logger.PrintInfo("[%s] expecting %d responsive host(s)", op.name, len(op.hosts))

	err := pollState(op, execContext)
	if err != nil {
		// show the hosts that aren't responding
		msg := fmt.Sprintf("Have not yet received OK from the hosts '%v', details: %s",
			util.SliceDiff(op.hosts, op.okHosts), err)
		op.logger.PrintError(msg)
		return errors.New(msg)
	}
	return nil
}

func (op *httpsPollCertificateHealthOp) shouldStopPolling() (bool, error) {
	var allErrs error
	op.okHosts = []string{} // reset this to avoid removing hosts that succeed, then fail

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		// if good, add to okHosts
		op.logResponse(host, result)

		// expected if HTTPS service can't verify the new client certs yet because
		// it's using the old trusted CA
		if result.isUnauthorizedRequest() {
			op.logger.Info("HTTPS service reports unauthorized request, continuing to poll", "host", host)
			continue
		}

		if !result.isPassing() {
			// a failure result other than 401 from the HTTPS service after connection succeeds is unexpected
			if result.isFailing() {
				allErrs = errors.Join(allErrs, result.err)
				continue
			}
			// expected if vclusterops validation of HTTPS service cert is enabled and
			// the service hasn't restarted yet, so the old cert is presented to vclusterops
			if result.isException() {
				op.logger.Info("Possible TLS exception while attempting to connect to HTTPS", "host", host, "error", result.err.Error())
				continue
			}
			// expected if service restarts while we are polling after handshake but before auth
			if result.isEOF() {
				op.logger.Info("Possible connection reset due to HTTPS service restart", "host", host, "error", result.err.Error())
				continue
			}
			// EoF, exception, and http return code != 200 are the only usual non-passing cases, so this is something unknown.
			// To be safe, keep retrying until the result changes or the op times out.
			op.logger.Info("Unknown non-passing result while attempting to connect to the HTTPS service", "host", host,
				"error", result.err.Error())
			continue
		}

		// the https service health endpoint has an empty body, so no need to check further
		op.okHosts = append(op.okHosts, host)
	}

	// return immediately if there are unexpected failures
	if allErrs != nil {
		return true, allErrs
	}

	// check to see if we've heard from all the hosts yet
	healthyNodeCount := len(op.okHosts)
	if healthyNodeCount < len(op.hosts) {
		op.logger.PrintInfo("[%s] %d host(s) responsive", op.name, healthyNodeCount)
		op.updateSpinnerMessage("%d host(s) responsive, expecting %d responsive host(s)", healthyNodeCount, len(op.hosts))
		return false, nil
	}

	op.logger.PrintInfo("[%s] All nodes are responsive", op.name)
	op.updateSpinnerStopMessage("all nodes are responsive")

	return true, nil
}
