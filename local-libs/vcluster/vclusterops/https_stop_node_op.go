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

type httpsStopNodeOp struct {
	opBase
	opHTTPSBase
	RequestParams map[string]string
	StopNodes     map[string]string // map with nodename as key and host as value
	nodeNames     []string          // node names in the target subcluster
}

func makeHTTPSStopNodeOp(hosts, nodeNames []string, useHTTPPassword bool, userName string,
	httpsPassword *string, timeout *int) (httpsStopNodeOp, error) {
	op := httpsStopNodeOp{}
	op.name = "HTTPSStopNodeOp"
	op.description = "Stop node"
	op.useHTTPPassword = useHTTPPassword
	op.hosts = hosts
	op.nodeNames = nodeNames

	// set the query params, "timeout" is optional
	op.RequestParams = make(map[string]string)
	if timeout != nil {
		op.RequestParams["timeout"] = strconv.Itoa(*timeout)
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

func makeHTTPSStopInputNodesOp(stopNodes map[string]string, useHTTPPassword bool, userName string,
	httpsPassword *string, timeout *int) (httpsStopNodeOp, error) {
	op, err := makeHTTPSStopNodeOp(nil, nil, useHTTPPassword, userName, httpsPassword, timeout)
	if err != nil {
		return op, err
	}
	op.StopNodes = stopNodes
	return op, nil
}

func (op *httpsStopNodeOp) setupClusterHTTPRequest(hosts, nodenames []string) error {
	for i, nodename := range nodenames {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.NodesEndpoint + nodename + util.ShutDownEndpoint)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.QueryParams = op.RequestParams
		op.clusterHTTPRequest.RequestCollection[hosts[i]] = httpRequest
	}

	return nil
}

func (op *httpsStopNodeOp) prepare(execContext *opEngineExecContext) error {
	var hosts, nodenames []string
	if len(op.hosts) == 0 && len(op.nodeNames) == 0 {
		if len(op.StopNodes) == 0 && len(execContext.nodesInfo) == 0 {
			return fmt.Errorf(`[%s] List of nodes to be stopped is empty`, op.name)
		}
		if len(op.StopNodes) == 0 {
			for _, node := range execContext.nodesInfo {
				nodenames = append(nodenames, node.Name)
				hosts = append(hosts, node.Address)
			}
		} else {
			for nodename, host := range op.StopNodes {
				nodenames = append(nodenames, nodename)
				hosts = append(hosts, host)
			}
		}
	} else {
		hosts = op.hosts
		nodenames = op.nodeNames
	}
	execContext.dispatcher.setup(hosts)

	return op.setupClusterHTTPRequest(hosts, nodenames)
}

func (op *httpsStopNodeOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

/*
Sample response for a successful stop command:

		{
	        "detail": ""
		}
*/
func (op *httpsStopNodeOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		// EOF is expected in node shutdown: we expect the node's HTTPS service to go down quickly
		// and the Server HTTPS service does not guarantee that the response being sent back to the client before it closes
		if result.isEOF() {
			continue
		}
		if !result.isPassing() {
			// If we can't connect to the host, it's already down. That's not an error
			// Note: We should improve the error handling here.
			//       VER-93730 tracks this issue
			if strings.Contains(result.err.Error(), "connection refused") {
				op.logger.PrintInfo("[%s] host %s is already down", op.name, host)
			} else {
				allErrs = errors.Join(allErrs, result.err)
			}
			continue
		}

		_, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			continue
		}
	}

	return allErrs
}

func (op *httpsStopNodeOp) finalize(_ *opEngineExecContext) error {
	return nil
}
