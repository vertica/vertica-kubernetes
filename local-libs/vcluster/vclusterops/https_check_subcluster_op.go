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
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsCheckSubclusterOp struct {
	opBase
	opHTTPSBase
	scName      string
	isSecondary bool
	ctlSetSize  int
	cmdType     CmdType
}

func makeHTTPSGetSubclusterInfoOp(useHTTPPassword bool, userName string, httpsPassword *string,
	scName string, cmdType CmdType) (httpsCheckSubclusterOp, error) {
	op := httpsCheckSubclusterOp{}
	op.name = "HTTPSCheckSubclusterOp"
	op.description = "Collect information for the specified subcluster"
	op.scName = scName
	op.cmdType = cmdType

	op.useHTTPPassword = useHTTPPassword

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
func makeHTTPSCheckSubclusterOp(useHTTPPassword bool, userName string, httpsPassword *string,
	scName string, isPrimary bool, ctlSetSize int) (httpsCheckSubclusterOp, error) {
	op, err := makeHTTPSGetSubclusterInfoOp(useHTTPPassword, userName, httpsPassword, scName, AddSubclusterCmd)
	if err != nil {
		return op, err
	}
	op.isSecondary = !isPrimary
	op.ctlSetSize = ctlSetSize
	return op, nil
}

func (op *httpsCheckSubclusterOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint(util.SubclustersEndpoint + op.scName)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsCheckSubclusterOp) prepare(execContext *opEngineExecContext) error {
	if len(execContext.upHosts) == 0 {
		return fmt.Errorf(`[%s] Cannot find any up hosts in OpEngineExecContext`, op.name)
	}
	execContext.dispatcher.setup(execContext.upHosts)

	return op.setupClusterHTTPRequest(execContext.upHosts)
}

func (op *httpsCheckSubclusterOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

// the following struct will store a subcluster's information for this op
type scInfo struct {
	SCName      string `json:"subcluster_name"`
	IsSecondary bool   `json:"is_secondary"`
	CtlSetSize  int    `json:"control_set_size"`
	Sandbox     string `json:"sandbox"`
	IsCritical  bool   `json:"is_critical"`
}

// Return true if all the results need to be scanned to figure out
// correct subcluster details
func completeScanRequired(cmdType CmdType) bool {
	return cmdType == StopSubclusterCmd
}

func (op *httpsCheckSubclusterOp) processResult(_ *opEngineExecContext) error {
	var err error
	isSubclusterCritical := false

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			// skip checking response from other nodes because we will get the same error there
			return result.err
		}
		if !result.isPassing() {
			err = result.err
			// try processing other hosts' responses when the current host has some server errors
			continue
		}

		// decode the json-format response
		// A successful response object will be like below:

		/*
			                   {
			                     "subcluster_name": "sc1",
				              "control_set_size": 2,
					      "is_secondary": true,
					      "is_default": false,
					      "sandbox": "",
					      "is_critical": false
					   }
		*/
		subclusterInfo := scInfo{}
		err = op.parseAndCheckResponse(host, result.content, &subclusterInfo)
		if err != nil {
			return fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
		}
		if op.cmdType == AddSubclusterCmd {
			err = op.verifySubclusterDetails(&subclusterInfo)
			if err != nil {
				return fmt.Errorf(`[%s] fail to verify subcluster info on host %s, details: %w`, op.name, host, err)
			}
		}

		// cache subcluster critical info for stop subcluster command
		if subclusterInfo.IsCritical {
			isSubclusterCritical = true
		}

		// early return if the command only needs response from one host
		if !completeScanRequired(op.cmdType) {
			return nil
		}
	}
	if op.cmdType == StopSubclusterCmd {
		if isSubclusterCritical {
			return fmt.Errorf(`[%s] subcluster %s is critical, shutting the subcluster down will cause the whole database/sandbox shutdown`,
				op.name, op.scName)
		}
	}

	return err
}

func (op *httpsCheckSubclusterOp) verifySubclusterDetails(subclusterInfo *scInfo) error {
	if subclusterInfo.SCName != op.scName {
		return fmt.Errorf(`[%s] new subcluster name should be '%s' but got '%s'`, op.name, op.scName, subclusterInfo.SCName)
	}
	if subclusterInfo.IsSecondary != op.isSecondary {
		if op.isSecondary {
			return fmt.Errorf(`[%s] new subcluster should be a secondary subcluster but got a primary subcluster`, op.name)
		}
		return fmt.Errorf(`[%s] new subcluster should be a primary subcluster but got a secondary subcluster`, op.name)
	}
	if subclusterInfo.CtlSetSize != op.ctlSetSize {
		return fmt.Errorf(`[%s] new subcluster should have control set size as %d but got %d`, op.name, op.ctlSetSize, subclusterInfo.CtlSetSize)
	}
	return nil
}
func (op *httpsCheckSubclusterOp) finalize(_ *opEngineExecContext) error {
	return nil
}
