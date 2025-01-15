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
	"strings"
	"time"

	"github.com/vertica/vcluster/rfc7807"
	"github.com/vertica/vcluster/vclusterops/util"
	"golang.org/x/exp/slices"
)

type opType int

const (
	CreateDB opType = iota
	DropDB
	StopDB
	StartDB
	ReviveDB
	StopSC
	ReIP

	checkDBRunningOpName    = "HTTPSCheckDBRunningOp"
	checkDBRunningOpDesc    = "Verify database is running"
	checkDBNotRunningOpDesc = "Verify database is not running"
)

func (op opType) String() string {
	switch op {
	case CreateDB:
		return "Create DB"
	case DropDB:
		return "Drop DB"
	case StopDB:
		return "Stop DB"
	case StartDB:
		return "Start DB"
	case ReviveDB:
		return "Revive DB"
	case StopSC:
		return "Stop Subcluster"
	case ReIP:
		return "Re-ip Hosts"
	}
	return "unknown operation"
}

var maskEOFOp = []opType{DropDB}

// DBIsRunningError is an error to indicate we found the database still running.
// This is emitted from this op. Callers can do type checking to perform an
// action based on the error.
type DBIsRunningError struct {
	Detail string
}

// Error returns the message details. This is added so that it is compatible
// with the error interface.
func (e *DBIsRunningError) Error() string {
	return e.Detail
}

type httpsCheckRunningDBOp struct {
	opBase
	opHTTPSBase
	opType      opType
	sandbox     string // check if DB is running on specified sandbox
	mainCluster bool   // check if DB is running on the main cluster.
}

func makeHTTPSCheckRunningDBOp(hosts []string,
	useHTTPPassword bool, userName string,
	httpsPassword *string, operationType opType,
) (httpsCheckRunningDBOp, error) {
	op := httpsCheckRunningDBOp{}
	op.name = checkDBRunningOpName
	op.description = checkDBRunningOpDesc
	op.hosts = hosts
	op.useHTTPPassword = useHTTPPassword
	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}

	op.userName = userName
	op.httpsPassword = httpsPassword
	op.opType = operationType
	if op.opType == StopDB {
		op.description = checkDBNotRunningOpDesc
	}
	return op, nil
}

func makeHTTPSCheckRunningDBOpWithoutHosts(useHTTPPassword bool, userName string,
	httpsPassword *string, operationType opType) (httpsCheckRunningDBOp, error) {
	return makeHTTPSCheckRunningDBOp(nil, useHTTPPassword, userName, httpsPassword, operationType)
}

func makeHTTPSCheckRunningDBWithSandboxOp(hosts []string,
	useHTTPPassword bool, userName string, sandbox string, mainCluster bool,
	httpsPassword *string, operationType opType,
) (httpsCheckRunningDBOp, error) {
	op, err := makeHTTPSCheckRunningDBOp(hosts, useHTTPPassword, userName, httpsPassword, operationType)
	if err != nil {
		return op, err
	}
	op.sandbox = sandbox         // check if DB is running on specified sandbox
	op.mainCluster = mainCluster // check if DB is running on the main cluster
	return op, nil
}

func (op *httpsCheckRunningDBOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("nodes")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsCheckRunningDBOp) logPrepare() {
	op.logger.Info("prepare() called", "opType", op.opType)
}

func (op *httpsCheckRunningDBOp) prepare(execContext *opEngineExecContext) error {
	// If no hosts passed in, we will find the hosts from execute-context
	if len(op.hosts) == 0 && op.opType == StopSC {
		// execContext.nodesInfo stores the information of UP nodes in target subcluster
		if len(execContext.nodesInfo) == 0 {
			return fmt.Errorf(`[%s] Cannot find any node information of target subcluster in OpEngineExecContext`, op.name)
		}
		hostsInSC := make([]string, 0, len(execContext.nodesInfo))
		for _, node := range execContext.nodesInfo {
			hostsInSC = append(hostsInSC, node.Address)
		}
		op.hosts = hostsInSC
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsCheckRunningDBOp) generateHintMessage(host, dbName string) (msg string) {
	generalMsg := fmt.Sprintf("[%s] Detected HTTPS service running on host %s", op.name, host)
	switch op.opType {
	case CreateDB:
		msg = fmt.Sprintf("%s, please stop the HTTPS service before creating a new database.", generalMsg)
	case DropDB:
		msg = fmt.Sprintf("%s, please stop the HTTPS service before dropping the existing database.", generalMsg)
	case ReIP:
		msg = fmt.Sprintf("%s, please consider using start_node to re-ip nodes for the running database.", generalMsg)
	case StopDB, StartDB, ReviveDB, StopSC:
		msg = fmt.Sprintf("%s.", generalMsg)
	}
	if dbName != "" {
		msg += fmt.Sprintf(" Database %s is still running on host %s", dbName, host)
	}
	return msg
}

/*
	https v1/nodes endpoint response examples
	 	- for a response with 200 status code:
		{
			'details':[],
			'node_list':[{
							'name': 'v_test_db_running_node0001',
							'node_id':'45035996273704982',
							'address': '192.168.1.101',
							'state' : 'UP',
							'database' : 'test_db',
							'is_primary' : true,
							'is_readonly' : false,
							'catalog_path' : "\/data\/test_db\/v_test_db_node0001_catalog\/Catalog",
							'subcluster_name' : '',
							'last_msg_from_node_at':'2023-01-23T15:18:18.44866",
							'down_since' : null,
							'build_info' : "v12.0.4-7142c8b01f373cc1aa60b1a8feff6c40bfb7afe8",
							'sandbox_name' : "sandbox"
						}]
		}

		- for a response with non-200 status code
		(i.e. an rfc error when the endpoint does not return a well-structured node list for some reason)
			- an example
			(this specific error indicates that the node is starting and hasn't pulled the latest catalog yet):
			{
			"type": "https:\/\/integrators.vertica.com\/rest\/errors\/unauthorized-request",
			"title": "Unauthorized-request",
			"detail": "Local node has not joined cluster yet, HTTP server will accept connections when the node has joined the cluster\n",
			"host": "0.0.0.0",
			"status": 401
			}
			- another example
			(this specific error indicates that the password provided for authentication is incorrect):
			{
			"type": "https:\/\/integrators.vertica.com\/rest\/errors\/unauthorized-request",
			"title": "Unauthorized-request",
			"detail": "Wrong password\n",
			"host": "0.0.0.0",
			"status": 401
			}
*/

func (op *httpsCheckRunningDBOp) isDBRunningOnHost(host string,
	nodesState *nodesStateInfo, result hostHTTPResult) (status, msg string, err error) {
	runningStatus := "running"
	startingStatus := "starting/waiting to join cluster"
	status = runningStatus
	runningDBName := ""
	// If request to /nodes is successful, get the dbname for a detailed message
	if result.isSuccess() {
		nodeList := nodesState.NodeList
		if len(nodeList) == 0 {
			// exception, throw an error
			noNodeErr := fmt.Errorf("[%s] Unexpected result from host %s: empty node_list obtained from /nodes endpoint response",
				op.name, host)
			return status, "", noNodeErr
		}
		nodeInfo := nodeList[0]
		runningDBName = nodeInfo.Database
	} else {
		// check whether the node is starting and hasn't pulled the latest catalog yet
		// setting status for logging purpose
		rfcError := &rfc7807.VProblem{}
		if ok := errors.As(result.err, &rfcError); ok &&
			rfcError.ProblemID == rfc7807.AuthenticationError &&
			strings.Contains(rfcError.Detail, "Local node has not joined cluster yet") {
			status = startingStatus
		}
	}
	msg = op.generateHintMessage(host, runningDBName)
	return status, msg, nil
}

func (op *httpsCheckRunningDBOp) accumulateSandboxedAndMainHosts(sandboxingHosts map[string]string,
	mainClusterHosts map[string]struct{}, nodesState *nodesStateInfo) {
	if op.sandbox == "" || !op.mainCluster {
		return
	}

	nodeList := nodesState.NodeList
	if len(nodeList) > 0 {
		for _, node := range nodeList {
			if node.Sandbox == op.sandbox && op.sandbox != "" {
				sandboxingHosts[node.Address] = node.State
			}
			if op.mainCluster && node.Sandbox == "" {
				mainClusterHosts[node.Address] = struct{}{}
			}
		}
	}
}

// processResult will look at all of the results that come back from the hosts.
// We don't return an error if all of the nodes are down. Otherwise, an error is
// returned.
func (op *httpsCheckRunningDBOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	// golang doesn't have set data structure,
	// so use maps for caching distinct up and down hosts
	// we have this list of hosts for better debugging info
	upHosts := make(map[string]bool)
	downHosts := make(map[string]bool)
	exceptionHosts := make(map[string]bool)
	sandboxedHosts := make(map[string]string)
	mainClusterHosts := make(map[string]struct{})
	// print msg
	msg := ""
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		// EOF is expected in node shutdown: we expect the node's HTTPS service to go down quickly
		// and the Server HTTPS service does not guarantee that the response being sent back to the client before it closes
		if result.isEOF() && slices.Contains(maskEOFOp, op.opType) {
			continue
		}
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
		}
		if result.isFailing() && !result.isHTTPRunning() {
			downHosts[host] = true
			continue
		} else if result.isException() || result.isEOF() {
			exceptionHosts[host] = true
			continue
		}

		upHosts[host] = true

		// a passing result means that the db isn't down
		nodesStates := nodesStateInfo{}
		err := op.parseAndCheckResponse(host, result.content, &nodesStates)
		// parsing shouldn't fail in normal circumstances (even if response is rfc error), checking for err
		// here just as a guardrail
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			msg = result.content
			continue
		}

		op.accumulateSandboxedAndMainHosts(sandboxedHosts, mainClusterHosts, &nodesStates)

		status, checkMsg, err := op.isDBRunningOnHost(host, &nodesStates, result)
		if err != nil {
			return fmt.Errorf("[%s] error happened during checking DB running on host %s, details: %w",
				op.name, host, err)
		}
		op.logger.Info("DB running", "host", host, "status", status, "checkMsg", checkMsg)
		// return at least one check msg to user
		msg = checkMsg
	}

	return op.handleDBRunning(allErrs, msg, upHosts, downHosts, exceptionHosts, sandboxedHosts, mainClusterHosts)
}

func (op *httpsCheckRunningDBOp) handleDBRunning(allErrs error, msg string, upHosts, downHosts, exceptionHosts map[string]bool,
	sandboxedHosts map[string]string, mainClusterHosts map[string]struct{}) error {
	op.logger.Info("check db running results", "up hosts", upHosts, "down hosts", downHosts, "hosts with status unknown", exceptionHosts,
		"sandboxed hosts", sandboxedHosts)

	dbDown := op.checkProcessedResult(sandboxedHosts, mainClusterHosts, upHosts)
	if dbDown {
		return nil
	}
	op.logger.Info("Check DB running", "detail", msg)

	switch op.opType {
	case CreateDB:
		const createDBMsg = "aborting database creation"
		op.logger.PrintInfo(createDBMsg)
		op.updateSpinnerMessage(createDBMsg)
	case DropDB:
		const dropDBMsg = "aborting database drop"
		op.logger.PrintInfo(dropDBMsg)
		op.updateSpinnerMessage(dropDBMsg)
	case StopDB:
		const stopDBMsg = "the database is not down yet"
		op.logger.PrintInfo(stopDBMsg)
		op.updateSpinnerMessage(stopDBMsg)
	case StopSC:
		const stopSCMsg = "the subcluster is not down yet"
		op.logger.PrintInfo(stopSCMsg)
		op.updateSpinnerMessage(stopSCMsg)
	case StartDB:
		const startDBMsg = "aborting database start"
		op.logger.PrintInfo(startDBMsg)
		op.updateSpinnerMessage(startDBMsg)
	case ReviveDB:
		const reviveDBMsg = "aborting database revival"
		op.logger.PrintInfo(reviveDBMsg)
		op.updateSpinnerMessage(reviveDBMsg)
	case ReIP:
		const reIPMsg = "aborting re-ip hosts"
		op.logger.PrintInfo(reIPMsg)
		op.updateSpinnerMessage(reIPMsg)
	}

	// when db is running, append an error to allErrs for stopping VClusterOpEngine
	return errors.Join(allErrs, &DBIsRunningError{Detail: msg})
}

func (op *httpsCheckRunningDBOp) checkProcessedResult(sandboxedHosts map[string]string,
	mainClusterHosts map[string]struct{}, upHosts map[string]bool) bool {
	// no DB is running on hosts, return a passed result
	if len(upHosts) == 0 {
		if op.sandbox != "" || op.mainCluster {
			op.logger.PrintWarning("All the nodes in the database are down")
		}
		return true
	}

	// Check if any of the sandboxed hosts is UP
	// sandboxedHosts would be empty if op.sandbox is ""
	isSandboxUp := false
	for host := range sandboxedHosts {
		if _, ok := upHosts[host]; ok {
			isSandboxUp = true
			break
		}
	}

	isMainHostUp := false
	for host := range mainClusterHosts {
		if _, ok := upHosts[host]; ok {
			isMainHostUp = true
			break
		}
	}

	// If all sandboxed hosts are down, DB is down for the given sandbox
	if !isSandboxUp && op.sandbox != "" {
		op.logger.Info("all hosts in the sandbox: " + op.sandbox + " are down")
		return true
	}
	if !isMainHostUp && op.mainCluster {
		op.logger.Info("all hosts in the main cluster are down")
		return true
	}
	return false
}

func (op *httpsCheckRunningDBOp) execute(execContext *opEngineExecContext) error {
	op.logger.Info("Execute() called", "opType", op.opType)
	switch op.opType {
	case CreateDB, StartDB, ReviveDB, ReIP, DropDB:
		return op.checkDBConnection(execContext)
	case StopDB, StopSC:
		return op.pollForDBDown(execContext)
	}

	return fmt.Errorf("unknown operation found in HTTPCheckRunningDBOp")
}

func (op *httpsCheckRunningDBOp) pollForDBDown(execContext *opEngineExecContext) error {
	// start the polling
	startTime := time.Now()
	// for tests
	timeoutSecondStr := util.GetEnv("NODE_STATE_POLLING_TIMEOUT", strconv.Itoa(StopDBTimeout))
	timeoutSecond, err := strconv.Atoi(timeoutSecondStr)
	if err != nil {
		return fmt.Errorf("invalid timeout value %s: %w", timeoutSecondStr, err)
	}

	// do not poll, just return succeed
	if timeoutSecond <= 0 {
		return nil
	}
	duration := time.Duration(timeoutSecond) * time.Second
	count := 0
	for endTime := startTime.Add(duration); ; {
		if time.Now().After(endTime) {
			break
		}
		if count > 0 {
			time.Sleep(PollingInterval * time.Second)
		}
		err = execContext.dispatcher.sendRequest(&op.clusterHTTPRequest, op.spinner)
		if err != nil {
			return fmt.Errorf("fail to dispatch request %v: %w", op.clusterHTTPRequest, err)
		}
		err = op.processResult(execContext)
		// If we get an error, intentionally eat the error so that we send the
		// request again. We are waiting for all nodes to be down, which is a
		// success result from processContext.
		if err != nil {
			op.logger.Info("failure when checking node status", "err", err)
		} else {
			return nil
		}
		count++
	}
	// timeout
	target := "DB"
	if op.opType == StopSC {
		target = "subcluster"
	}
	msg := fmt.Sprintf("the %s is still up after %s seconds", target, timeoutSecondStr)
	op.logger.PrintWarning(msg)
	return errors.New(msg)
}

func (op *httpsCheckRunningDBOp) checkDBConnection(execContext *opEngineExecContext) error {
	err := execContext.dispatcher.sendRequest(&op.clusterHTTPRequest, op.spinner)
	if err != nil {
		return fmt.Errorf("fail to dispatch request %v: %w", op.clusterHTTPRequest, err)
	}
	return op.processResult(execContext)
}

func (op *httpsCheckRunningDBOp) finalize(_ *opEngineExecContext) error {
	return nil
}
