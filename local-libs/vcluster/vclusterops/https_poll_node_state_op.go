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
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

// Timeout set to 30 seconds for each GET /v1/nodes/{node} call.
// 30 seconds is long enough for normal http request.
// If this timeout is reached, it might imply that the target IP is unreachable
const defaultHTTPSRequestTimeoutSeconds = 30

type httpsPollNodeStateOp struct {
	opBase
	opHTTPSBase
	currentHost string
	// The timeout for the entire operation (polling)
	timeout int
	// The timeout for each http request. Requests will be repeated if timeout hasn't been exceeded.
	httpRequestTimeout int
	cmdType            CmdType
	// poll for nodes down: Set to true if nodes need to be polled to be down
	checkDown bool
	// Pointer to list of permanent hosts, as identified by a preceding op
	permanentHosts *[]string
}

func makeHTTPSPollNodeStateOpHelper(hosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string) (httpsPollNodeStateOp, error) {
	op := httpsPollNodeStateOp{}
	op.name = "HTTPSPollNodeStateOp"
	op.description = fmt.Sprintf("Wait for %d node(s) to come up", len(hosts))
	op.hosts = hosts
	op.useHTTPPassword = useHTTPPassword
	op.httpRequestTimeout = defaultHTTPSRequestTimeoutSeconds
	op.checkDown = false // setting default to poll nodes UP
	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

func makeHTTPSPollNodeStateDownOp(hosts []string,
	useHTTPPassword bool, userName string,
	httpsPassword *string) (httpsPollNodeStateOp, error) {
	op, err := makeHTTPSPollNodeStateOpHelper(hosts, useHTTPPassword, userName, httpsPassword)
	if err != nil {
		return op, err
	}
	op.timeout = util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", StartupPollingTimeout)
	op.checkDown = true
	op.description = fmt.Sprintf("Wait for %d node(s) to go DOWN", len(hosts))
	return op, nil
}

func makeHTTPSPollNodeStateOp(hosts []string,
	useHTTPPassword bool, userName string,
	httpsPassword *string, timeout int) (httpsPollNodeStateOp, error) {
	op, err := makeHTTPSPollNodeStateOpHelper(hosts, useHTTPPassword, userName, httpsPassword)
	if err != nil {
		return op, err
	}

	if timeout == 0 {
		// using default value
		op.timeout = util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", StartupPollingTimeout)
	} else {
		op.timeout = timeout
	}
	return op, err
}

// makeHTTPSPollPermanentNodeStateOp will filter out non-permanent hosts from
// polling, as identified dynamically by a previous op
func makeHTTPSPollPermanentNodeStateOp(hosts []string,
	permanentHosts *[]string, useHTTPPassword bool, userName string,
	httpsPassword *string, timeout int) (httpsPollNodeStateOp, error) {
	op, err := makeHTTPSPollNodeStateOp(hosts, useHTTPPassword, userName, httpsPassword, timeout)
	if err != nil {
		return op, err
	}
	op.permanentHosts = permanentHosts
	return op, nil
}

func (op *httpsPollNodeStateOp) getPollingTimeout() int {
	return max(op.timeout, 0)
}

func (op *httpsPollNodeStateOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.Timeout = op.httpRequestTimeout
		httpRequest.buildHTTPSEndpoint(util.NodesEndpoint + host)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsPollNodeStateOp) prepare(execContext *opEngineExecContext) error {
	// if needed, filter out hosts that can't be polled
	if op.permanentHosts != nil {
		op.hosts = util.SliceCommon(op.hosts, *op.permanentHosts)
		// if all hosts started were compute nodes, nothing to do here
		if len(op.hosts) == 0 {
			op.skipExecute = true
			return nil
		}
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsPollNodeStateOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsPollNodeStateOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *httpsPollNodeStateOp) processResult(execContext *opEngineExecContext) error {
	op.logger.PrintInfo("[%s] expecting %d %s host(s)", op.name, len(op.hosts), checkStatusToString(op.checkDown))

	err := pollState(op, execContext)
	if err != nil {
		// show the host that is not UP
		msg := fmt.Sprintf("Cannot get the correct response from the host %s after %d seconds, details: %s",
			op.currentHost, op.timeout, err)
		op.logger.PrintError(msg)
		return errors.New(msg)
	}
	return nil
}

func (op *httpsPollNodeStateOp) shouldStopPolling() (bool, error) {
	if op.checkDown {
		return op.shouldStopPollingForDown()
	}
	upNodeCount := 0

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.currentHost = host

		// when we get timeout error, we know that the host is unreachable/dead
		if result.isTimeout() {
			return true, fmt.Errorf("[%s] cannot connect to host %s, please check if the host is still alive", op.name, host)
		}

		// VER-88185 vcluster start_db - password related issues
		// We don't need to wait until timeout to determine if all nodes are up or not.
		// If we find the wrong password for the HTTPS service on any hosts, we should fail immediately.
		// We also need to let user know to wait until all nodes are up
		if result.isPasswordAndCertificateError(op.logger) {
			if op.cmdType == StartDBCmd || op.cmdType == StartNodeCmd {
				op.logger.PrintError("[%s] The credentials are incorrect. 'Catalog Sync' will not be executed.",
					op.name)
				return false, makePollNodeStateAuthenticationError(op.name, host)
			} else if op.cmdType == CreateDBCmd {
				return true, fmt.Errorf("[%s] wrong password/certificate for https service on host %s",
					op.name, host)
			}
		}
		if result.isPassing() {
			// parse the /nodes/{node} endpoint response
			nodesInformation := nodesInfo{}
			err := op.parseAndCheckResponse(host, result.content, &nodesInformation)
			if err != nil {
				op.logger.PrintError("[%s] fail to parse result on host %s, details: %s",
					op.name, host, err)
				return true, err
			}

			// check whether the node is up
			// the node list should only have one node info
			if len(nodesInformation.NodeList) == 1 {
				nodeInfo := nodesInformation.NodeList[0]
				if nodeInfo.State == util.NodeUpState {
					upNodeCount++
				}
			} else {
				// if HTTPS endpoint cannot function well on any of the hosts, we do not want to retry polling
				return true, fmt.Errorf(util.NodeInfoCountMismatch, op.name, len(nodesInformation.NodeList), host)
			}
		}
	}

	if upNodeCount < len(op.hosts) {
		op.logger.PrintInfo("[%s] %d host(s) up", op.name, upNodeCount)
		op.updateSpinnerMessage("%d host(s) up, expecting %d up host(s)", upNodeCount, len(op.hosts))
		return false, nil
	}

	op.logger.PrintInfo("[%s] All nodes are up", op.name)
	op.updateSpinnerStopMessage("all nodes are up")

	return true, nil
}

func (op *httpsPollNodeStateOp) shouldStopPollingForDown() (bool, error) {
	upNodeCount := 0
	upHosts := make(map[string]bool)
	exceptionHosts := make(map[string]bool)
	downHosts := make(map[string]bool)
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.currentHost = host

		// when we get timeout error, we know that the host is unreachable/dead
		if result.isTimeout() {
			return true, fmt.Errorf("[%s] cannot connect to host %s, please check if the host is still alive", op.name, host)
		}

		// We don't need to wait until timeout to determine if all nodes are down or not.
		// If we find the wrong password for the HTTPS service on any hosts, we should fail immediately.
		// We also need to let user know to wait until all nodes are down
		if result.isPasswordAndCertificateError(op.logger) {
			return true, fmt.Errorf("[%s] wrong password/certificate for https service on host %s",
				op.name, host)
		}
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
		}
		if result.isFailing() && !result.isHTTPRunning() {
			downHosts[host] = true
			continue
		} else if result.isException() {
			exceptionHosts[host] = true
			continue
		}

		upHosts[host] = true
		upNodeCount++
	}

	if upNodeCount != 0 {
		op.logger.PrintInfo("[%s] %d host(s) up", op.name, upNodeCount)
		op.updateSpinnerMessage("%d host(s) up, expecting %d host(s) to be down", upNodeCount, len(op.hosts))
		return false, nil
	}
	op.logger.PrintInfo("[%s] All nodes are down", op.name)
	op.updateSpinnerStopMessage("all nodes are down")

	return true, nil
}

func makePollNodeStateAuthenticationError(opName, hostName string) error {
	return fmt.Errorf("[%s] wrong password/certificate for https service on host %s, but the nodes' startup have been in progress. "+
		"Please use vsql to check the nodes' status and manually run sync_catalog vsql command 'select sync_catalog()'", opName, hostName)
}
