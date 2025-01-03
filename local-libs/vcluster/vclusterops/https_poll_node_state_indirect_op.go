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
	"slices"
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
)

// httpsPollNodeStateIndirectOp allows polling for the state of nodes when those nodes certainly
// or possibly cannot be polled directly for their state.  For example, compute nodes, or nodes of
// unknown type.  Instead of calling the nodes endpoint directly on each host for info about only
// that node, it calls the nodes endpoint for all nodes on a separate slice of hosts (e.g. primary
// UP nodes in the same sandbox) which should know the states of all the nodes being checked.
type httpsPollNodeStateIndirectOp struct {
	opBase
	opHTTPSBase
	// Map of hosts to state to compute permanent and/or non-up nodes as needed
	checkedHostsToState map[string]string
	// The timeout for the entire operation (polling)
	timeout int
	// The timeout for each http request. Requests will be repeated if timeout hasn't been exceeded.
	httpRequestTimeout int
	// Node states considered final and ok when polling
	allowedStates []string
	// Pointer to output slice of permanent UP nodes identified by this op
	permanentNodes *[]string
}

var errNoUpNodesForPolling = errors.New("polling node state indirectly requires at least one primary up host")

func makeHTTPSPollNodeStateIndirectOpHelper(hosts, hostsToCheck []string,
	useHTTPPassword bool, userName string, httpsPassword *string,
	timeout int) (httpsPollNodeStateIndirectOp, error) {
	op := httpsPollNodeStateIndirectOp{}
	op.name = "HTTPSPollNodeStateIndirectOp"
	op.hosts = hosts // should be 1+ hosts capable of retrieving accurate node states, e.g. primary up hosts
	if op.hosts != nil && len(op.hosts) < 1 {
		return op, errNoUpNodesForPolling
	}
	op.checkedHostsToState = make(map[string]string, len(hostsToCheck))
	for _, host := range hostsToCheck {
		op.checkedHostsToState[host] = ""
	}
	if timeout == 0 {
		// using default value
		op.timeout = util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", StartupPollingTimeout)
	} else {
		op.timeout = timeout
	}
	op.useHTTPPassword = useHTTPPassword
	op.httpRequestTimeout = defaultHTTPSRequestTimeoutSeconds
	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

// makeHTTPSPollComputeNodeStateOp constructs a httpsPollNodeStateIndirectOp to poll
// until a known set of compute nodes are up (COMPUTE).
func makeHTTPSPollComputeNodeStateOp(hosts, computeHosts []string,
	useHTTPPassword bool, userName string,
	httpsPassword *string, timeout int) (httpsPollNodeStateIndirectOp, error) {
	op, err := makeHTTPSPollNodeStateIndirectOpHelper(hosts, computeHosts, useHTTPPassword,
		userName, httpsPassword, timeout)
	if err != nil {
		return op, err
	}
	op.allowedStates = []string{util.NodeComputeState} // poll for COMPUTE state (UP equivalent)
	op.description = fmt.Sprintf("Wait for %d compute node(s) to reach COMPUTE state", len(computeHosts))
	return op, err
}

// makeHTTPSPollUnknownNodeStateOp constructs a httpsPollNodeStateIndirectOp for polling
// until a set of nodes are up (COMPUTE or UP).  It also identifies the non-compute nodes for
// further operations.
func makeHTTPSPollUnknownNodeStateOp(hostsToCheck []string,
	permanentNodes *[]string, useHTTPPassword bool, userName string,
	httpsPassword *string, timeout int) (httpsPollNodeStateIndirectOp, error) {
	// get hosts from execContext later
	op, err := makeHTTPSPollNodeStateIndirectOpHelper(nil, hostsToCheck, useHTTPPassword,
		userName, httpsPassword, timeout)
	if err != nil {
		return op, err
	}
	op.permanentNodes = permanentNodes
	op.allowedStates = []string{util.NodeComputeState, util.NodeUpState} // poll for any valid up state
	op.description = "Wait for permanent and/or compute node(s) to come up"
	return op, nil
}

func (op *httpsPollNodeStateIndirectOp) getPollingTimeout() int {
	return max(op.timeout, 0)
}

func (op *httpsPollNodeStateIndirectOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.Timeout = op.httpRequestTimeout
		httpRequest.buildHTTPSEndpoint(util.NodesEndpoint)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsPollNodeStateIndirectOp) prepare(execContext *opEngineExecContext) error {
	if op.hosts == nil {
		if len(execContext.upHosts) < 1 {
			return errNoUpNodesForPolling
		}
		op.hosts = execContext.upHosts
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsPollNodeStateIndirectOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsPollNodeStateIndirectOp) finalize(_ *opEngineExecContext) error {
	return nil
}

// checkStatusToString formats the node state set in a user-readable way
func (op *httpsPollNodeStateIndirectOp) checkStatusToString() string {
	// only a short slice, no need for a string builder
	statusStr := ""
	for i, status := range op.allowedStates {
		if i > 0 {
			statusStr += " or "
		}
		if status == util.NodeComputeState {
			statusStr += "up (compute)"
		} else {
			statusStr += strings.ToLower(status)
		}
	}
	return statusStr
}

func (op *httpsPollNodeStateIndirectOp) getRemainingHostsString() string {
	var remainingHosts []string
	for host, state := range op.checkedHostsToState {
		if !slices.Contains(op.allowedStates, state) {
			remainingHosts = append(remainingHosts, host)
		}
	}
	return strings.Join(remainingHosts, ",")
}

func (op *httpsPollNodeStateIndirectOp) processResult(execContext *opEngineExecContext) error {
	op.logger.PrintInfo("[%s] expecting %d %s host(s)", op.name, len(op.checkedHostsToState), op.checkStatusToString())

	err := pollState(op, execContext)
	if err != nil {
		// show the hosts that are not up
		msg := fmt.Sprintf("the hosts [%s] are not in %s state after %d seconds, details: %s",
			op.getRemainingHostsString(), op.checkStatusToString(), op.timeout, err)
		op.logger.PrintError(msg)
		return errors.New(msg)
	}
	// if the permanent nodes list is needed, extract it
	if op.permanentNodes != nil {
		allowedPermanentStates := util.SliceDiff(op.allowedStates, []string{util.NodeComputeState})
		for host, state := range op.checkedHostsToState {
			if slices.Contains(allowedPermanentStates, state) {
				*op.permanentNodes = append(*op.permanentNodes, host)
			}
		}
	}
	return nil
}

func (op *httpsPollNodeStateIndirectOp) shouldStopPolling() (bool, error) {
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		// when we get timeout error, we know that the host is unreachable/dead
		if result.isTimeout() {
			return true, fmt.Errorf("[%s] cannot connect to host %s, please check if the host is still alive", op.name, host)
		}

		// We don't need to wait until timeout to determine if all nodes are up or not.
		// If we find the wrong password for the HTTPS service on any hosts, we should fail immediately.
		// We also need to let user know to wait until all nodes are up
		if result.isPasswordAndCertificateError(op.logger) {
			op.logger.PrintError("[%s] The credentials are incorrect. 'Catalog Sync' will not be executed.",
				op.name)
			return false, makePollNodeStateAuthenticationError(op.name, host)
		}
		if result.isPassing() {
			// parse the /nodes endpoint response for all nodes, then look for the specified ones
			nodesInformation := nodesInfo{}
			err := op.parseAndCheckResponse(host, result.content, &nodesInformation)
			if err != nil {
				op.logger.PrintError("[%s] fail to parse result on host %s, details: %s",
					op.name, host, err)
				return true, err
			}

			// check which nodes have desired state, e.g. COMPUTE, UP, etc.
			upNodeCount := 0
			for _, nodeInfo := range nodesInformation.NodeList {
				_, ok := op.checkedHostsToState[nodeInfo.Address]
				if !ok {
					// skip unrelated nodes
					continue
				}
				if slices.Contains(op.allowedStates, nodeInfo.State) {
					upNodeCount++
				}
				// stash state regardless of up/down/compute/etc.  it would be weird for a
				// previously up node to change status while we're still polling, but no
				// reason not to use the updated value in case it differs.
				op.checkedHostsToState[nodeInfo.Address] = nodeInfo.State
			}
			if upNodeCount == len(op.checkedHostsToState) {
				op.logger.PrintInfo("[%s] All nodes are %s", op.name, op.checkStatusToString())
				op.updateSpinnerStopMessage("all nodes are %s", op.checkStatusToString())
				return true, nil
			}
			// try the next host's result
			op.logger.PrintInfo("[%s] %d host(s) %s", op.name, upNodeCount, op.checkStatusToString())
			op.updateSpinnerMessage("%d host(s) %s, expecting %d %s host(s)",
				upNodeCount, op.checkStatusToString(), len(op.checkedHostsToState), op.checkStatusToString())
		}
	}
	// no host returned all new nodes as acceptable states, so keep polling
	return false, nil
}
