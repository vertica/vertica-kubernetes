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
	"strconv"

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsPollSubclusterNodeStateOp struct {
	opBase
	opHTTPSBase
	currentHost string
	timeout     int
	scName      string
	checkDown   bool
}

// This op is used to poll for nodes that are a part of the subcluster `scName` to be UP.
// A default timeout value defined by StartupPollingTimeout is applied. The user can suggest
// an alternate timeout through the env var NODE_STATE_POLLING_TIMEOUT
func makeHTTPSPollSubclusterNodeStateOp(scName string,
	useHTTPPassword bool, userName string,
	httpsPassword *string) (httpsPollSubclusterNodeStateOp, error) {
	op := httpsPollSubclusterNodeStateOp{}
	op.name = "HTTPSPollSubclusterNodeStateOp"
	op.description = "Wait for subcluster nodes"
	op.scName = scName
	op.useHTTPPassword = useHTTPPassword

	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.userName = userName
	op.httpsPassword = httpsPassword
	timeoutSecondStr := util.GetEnv("NODE_STATE_POLLING_TIMEOUT", strconv.Itoa(StartupPollingTimeout))
	timeoutSecond, err := strconv.Atoi(timeoutSecondStr)
	if err != nil {
		return httpsPollSubclusterNodeStateOp{}, err
	}
	op.timeout = timeoutSecond
	return op, nil
}

func makeHTTPSPollSubclusterNodeStateUpOp(hosts []string, scName string, timeout int,
	useHTTPPassword bool, userName string,
	httpsPassword *string) (httpsPollSubclusterNodeStateOp, error) {
	op, err := makeHTTPSPollSubclusterNodeStateOp(scName, useHTTPPassword, userName, httpsPassword)
	op.checkDown = false
	op.description += " to come up"
	op.hosts = hosts
	if timeout != 0 {
		op.timeout = timeout
	}
	return op, err
}

func makeHTTPSPollSubclusterNodeStateDownOp(hosts []string, scName string,
	useHTTPPassword bool, userName string,
	httpsPassword *string) (httpsPollSubclusterNodeStateOp, error) {
	op, err := makeHTTPSPollSubclusterNodeStateOp(scName, useHTTPPassword, userName, httpsPassword)
	op.checkDown = true
	op.description += " to come down"
	op.hosts = hosts
	return op, err
}

func (op *httpsPollSubclusterNodeStateOp) getPollingTimeout() int {
	// a negative value indicates no timeout and should never be used for this op
	return max(op.timeout, 0)
}

func (op *httpsPollSubclusterNodeStateOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.Timeout = defaultHTTPSRequestTimeoutSeconds
		httpRequest.buildHTTPSEndpoint(util.NodesEndpoint + host)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}
	return nil
}

func (op *httpsPollSubclusterNodeStateOp) prepare(execContext *opEngineExecContext) error {
	// We need to ensure that the https request to fetch the node state goes to the sandboxed node
	// because the main cluster will report the status of sandboxed nodes as "UNKNOWN".
	if len(op.hosts) == 0 {
		for _, vnode := range execContext.scNodesInfo {
			op.hosts = append(op.hosts, vnode.Address)
		}
	}
	// if there are still no hosts to check, e.g. empty subcluster, skip the op
	if len(op.hosts) == 0 {
		op.logger.PrintInfo("[%s] No nodes to poll for. Skipping operation.", op.name)
		op.skipExecute = true
		return nil
	}
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsPollSubclusterNodeStateOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}
	return op.processResult(execContext)
}

func (op *httpsPollSubclusterNodeStateOp) finalize(_ *opEngineExecContext) error {
	return nil
}

/*
sample https nodes/host endpoint response:

	 {
	  "detail": null,
	  "node_list": [{ "name": "v_platform_test_db_node0001",
	                  "node_id": 45035996273704986,
	                  "address": "192.168.1.101",
	                  "state": "UP",
	                  "database": "platform_test_db",
	                  "is_primary": true,
	                  "is_readonly": false,
	                  "catalog_path": "\/data\/platform_test_db\/v_platform_test_db_node0001_catalog\/Catalog",
	                  "data_path": ["\/data\/platform_test_db\/v_platform_test_db_node0001_data"],
	                  "depot_path": "\/data\/platform_test_db\/v_platform_test_db_node0001_depot",
	                  "subcluster_name": "default_subcluster",
	                  "last_msg_from_node_at": "2024-01-09T22:37:46.02982",
	                  "down_since": null,
	                  "build_info": "v24.2.0-63169351518aa4f02a6475ba33bdda142bfb659a"
	    }
	  ]
	}
*/
func (op *httpsPollSubclusterNodeStateOp) processResult(execContext *opEngineExecContext) error {
	op.logger.PrintInfo("[%s] expecting %d %s host(s)", op.name, len(op.hosts), checkStatusToString(op.checkDown))
	op.logger.Info("Processing Poll subcluster node state")
	err := pollState(op, execContext)
	if err != nil {
		// show the host that is not UP
		msg := fmt.Sprintf("Cannot get the correct response from the host %s after %d seconds, details: %s",
			op.currentHost, op.timeout, err)
		return errors.New(msg)
	}
	return nil
}

func checkStatusToString(checkDown bool) string {
	var checkString string
	if checkDown {
		checkString = "down"
	} else {
		checkString = "up"
	}
	return checkString
}

func (op *httpsPollSubclusterNodeStateOp) shouldStopPolling() (bool, error) {
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

		// We don't need to wait until timeout to determine if all nodes are up or not.
		// If we find the wrong password for the HTTPS service on any hosts, we should fail immediately.
		// We also need to let user know to wait until all nodes are up
		if result.isPasswordAndCertificateError(op.logger) {
			return true, fmt.Errorf("[%s] wrong password/certificate for https service on host %s",
				op.name, host)
		}
		if result.isPassing() {
			// parse the /nodes/{node} endpoint response
			nodesInformation := nodesInfo{}
			err := op.parseAndCheckResponse(host, result.content, &nodesInformation)
			if err != nil {
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
				// if NMA endpoint cannot function well on any of the hosts, we do not want to retry polling
				return true, fmt.Errorf("[%s] expect one node's information, but got %d nodes' information"+
					" from NMA /v1/nodes/{node} endpoint on host %s",
					op.name, len(nodesInformation.NodeList), host)
			}
		}
	}

	if upNodeCount < len(op.hosts) {
		op.logger.PrintInfo("[%s] %d host(s) up", op.name, upNodeCount)
		return false, nil
	}

	op.logger.PrintInfo("[%s] All nodes are up", op.name)

	return true, nil
}

func (op *httpsPollSubclusterNodeStateOp) shouldStopPollingForDown() (bool, error) {
	upNodeCount := 0
	upHosts := make(map[string]bool)
	exceptionHosts := make(map[string]bool)
	downHosts := make(map[string]bool)
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.currentHost = host

		// We don't need to wait until timeout to determine if all nodes are Down or not.
		// If we find the wrong password for the HTTPS service on any hosts, we should fail immediately.
		// We also need to let user know to wait until all nodes are DOWN
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
		return false, nil
	}

	op.logger.PrintInfo("[%s] All nodes are down", op.name)

	return true, nil
}
