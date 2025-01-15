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

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsStopSCOp struct {
	opBase
	opHTTPSBase
	scName        string
	force         bool
	requestParams map[string]string
}

func makeHTTPSStopSCOp(useHTTPPassword bool, userName string,
	httpsPassword *string, scName string, timeout int, force bool) (httpsStopSCOp, error) {
	op := httpsStopSCOp{}
	op.name = "HTTPSStopSCOp"
	op.description = "Stop subcluster"
	op.scName = scName
	op.force = force
	op.useHTTPPassword = useHTTPPassword

	// set the query params
	// If this is a force shutdown, we do not set "timeout" to make a shutdown without draining.
	// Otherwise, we set "timeout" to make a shutdown with draining.
	if !op.force {
		op.requestParams = make(map[string]string)
		op.requestParams["timeout"] = strconv.Itoa(timeout)
	}

	if useHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
		if err != nil {
			return op, err
		}
		op.userName = userName
		op.httpsPassword = httpsPassword
	}
	return op, nil
}

func (op *httpsStopSCOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.SubclustersEndpoint + op.scName + util.ShutDownEndpoint)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.QueryParams = op.requestParams
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsStopSCOp) prepare(execContext *opEngineExecContext) error {
	// execContext.nodesInfo stores the information of UP nodes in target subcluster
	if len(execContext.nodesInfo) == 0 {
		return fmt.Errorf(`[%s] Cannot find any node information of target subcluster in OpEngineExecContext`, op.name)
	}
	// send stop subcluster request to one UP host of the subcluster
	op.hosts = []string{execContext.nodesInfo[0].Address}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsStopSCOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsStopSCOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		// EOF is expected in node shutdown: we expect the node's HTTPS service to go down quickly
		// and the Server HTTPS service does not guarantee that the response being sent back to the client before it closes
		if result.isEOF() {
			continue
		}
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// decode the json-format response
		// The successful response object will be a dictionary:
		// 1. shutdown without drain
		// {
		// 	 "detail": ""
		// }
		// 2. shutdown with drain
		// case 1: no alive connection exists when shutdown subcluster
		// {
		//   "detail": "Shutdown message sent to subcluster (sc1)\n\n"
		// }
		// case 2: alive connection exists when shutdown subcluster
		// {
		// 	"detail": "Set subcluster (sc1) to draining state\nWaited for 1 nodes to drain\nShutdown message sent to subcluster (sc1)\n\n"
		// }
		// 3. shutdown a subcluster that is already down
		// {
		//   "detail": "No action taken: all nodes in subcluster sc1 are not connected to the database group.\n"
		// }
		response, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// verify if the endpoint returns correct successful message
		if !op.force {
			expectedDetails := "Shutdown message sent to subcluster (" + op.scName + ")"
			if !strings.Contains(response["detail"], expectedDetails) {
				err = fmt.Errorf(`[%s] response detail should like '... Shutdown message sent to subcluster ...' but got '%s'`,
					op.name, response["detail"])
				allErrs = errors.Join(allErrs, err)
			}
		} else if response["detail"] != "" {
			err = fmt.Errorf(`[%s] response detail should be empty but got '%s'`, op.name, response["detail"])
			allErrs = errors.Join(allErrs, err)
		}
	}

	return allErrs
}

func (op *httpsStopSCOp) finalize(_ *opEngineExecContext) error {
	return nil
}
