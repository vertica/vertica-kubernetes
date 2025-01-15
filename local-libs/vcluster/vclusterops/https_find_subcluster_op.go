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
)

type httpsFindSubclusterOp struct {
	opBase
	opHTTPSBase
	scName         string
	ignoreNotFound bool
	cmdType        CmdType
}

// makeHTTPSFindSubclusterOp initializes an op to find
// a subcluster by name and find the default subcluster.
// When ignoreNotFound is true, the op will not error out if
// the given cluster name is not found.
func makeHTTPSFindSubclusterOp(hosts []string, useHTTPPassword bool,
	userName string, httpsPassword *string, scName string,
	ignoreNotFound bool, cmdType CmdType,
) (httpsFindSubclusterOp, error) {
	op := httpsFindSubclusterOp{}
	op.name = "HTTPSFindSubclusterOp"
	op.description = "Collect subcluster information"
	op.hosts = hosts
	op.scName = scName
	op.ignoreNotFound = ignoreNotFound
	op.cmdType = cmdType

	err := op.validateAndSetUsernameAndPassword(op.name, useHTTPPassword, userName,
		httpsPassword)

	return op, err
}

func (op *httpsFindSubclusterOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("subclusters")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsFindSubclusterOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsFindSubclusterOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

// the following struct will store a subcluster's information for this op
type subclusterInfo struct {
	SCName    string `json:"subcluster_name"`
	IsDefault bool   `json:"is_default"`
	Sandbox   string `json:"sandbox"`
}

type scResp struct {
	SCInfoList []subclusterInfo `json:"subcluster_list"`
}

func (op *httpsFindSubclusterOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			// skip checking response from other nodes because we will get the same error there
			return result.err
		}
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			// try processing other hosts' responses when the current host has some server errors
			continue
		}

		// decode the json-format response
		// A successful response object will be like below:
		/*
			{
				"subcluster_list": [
					{
						"subcluster_name": "default_subcluster",
						"control_set_size": -1,
						"is_secondary": false,
						"is_default": true,
						"sandbox": ""
					},
					{
						"subcluster_name": "sc1",
						"control_set_size": 2,
						"is_secondary": true,
						"is_default": false,
						"sandbox": ""
					}
				]
			}
		*/
		subclusterResp := scResp{}
		err := op.parseAndCheckResponse(host, result.content, &subclusterResp)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			return allErrs
		}

		// process subclusters
		if err := op.processSubclusters(subclusterResp, execContext); err != nil {
			allErrs = errors.Join(allErrs, err)
			return allErrs
		}

		// good response from one node is enough for us
		return nil
	}
	return allErrs
}

func (op *httpsFindSubclusterOp) processSubclusters(subclusterResp scResp, execContext *opEngineExecContext) error {
	// 1. when subcluster name is given, look for the name in the database
	//    error out if not found
	// 2. look for the default subcluster, error out if not found
	foundNamedSc := false
	foundDefaultSc := false
	isSandboxed := false

	for _, scInfo := range subclusterResp.SCInfoList {
		if scInfo.SCName == op.scName {
			foundNamedSc = true
			if scInfo.Sandbox != "" {
				isSandboxed = true
			}
			op.logger.Info(`subcluster exists in the database`, "subcluster", scInfo.SCName, "dbName", op.name, "sandbox", scInfo.Sandbox)
		}

		if scInfo.IsDefault {
			// Store the default sc name into execContext
			foundDefaultSc = true
			execContext.defaultSCName = scInfo.SCName
			op.logger.Info(`found default subcluster in the database`, "subcluster", scInfo.SCName, "dbName", op.name)
		}

		if foundNamedSc && foundDefaultSc {
			break
		}
	}

	if op.scName != "" && !op.ignoreNotFound {
		if !foundNamedSc {
			return fmt.Errorf(`[%s] subcluster '%s' does not exist in the database`, op.name, op.scName)
		}
	}

	if !foundDefaultSc {
		return fmt.Errorf(`[%s] cannot find a default subcluster in the database`, op.name)
	}

	if isSandboxed {
		if op.cmdType == AddNodeCmd {
			return fmt.Errorf(`[%s] cannot add node into a sandboxed subcluster`, op.name)
		} else if op.cmdType == RemoveSubclusterCmd {
			return fmt.Errorf(`[%s] cannot remove a sandboxed subcluster, must unsandbox the subcluster first`, op.name)
		}
		return fmt.Errorf(`[%s] sandbox handling in the operation is not implemented`, op.name)
	}

	return nil
}

func (op *httpsFindSubclusterOp) finalize(_ *opEngineExecContext) error {
	return nil
}
