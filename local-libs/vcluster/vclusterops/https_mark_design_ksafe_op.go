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

const zeroSafeRspStr = "Marked design 0-safe"
const oneSafeRspStr = "Marked design 1-safe"

type httpsMarkDesignKSafeOp struct {
	opBase
	opHTTPSBase
	RequestParams map[string]string
	ksafeValue    int
}

func makeHTTPSMarkDesignKSafeOp(
	hosts []string,
	useHTTPPassword bool,
	userName string,
	httpsPassword *string,
	ksafeValue int,
) (httpsMarkDesignKSafeOp, error) {
	op := httpsMarkDesignKSafeOp{}
	op.name = "HTTPSMarkDesignKsafeOp"
	op.description = "Set k-safety"
	op.hosts = hosts
	op.useHTTPPassword = useHTTPPassword

	// set ksafeValue.  Should be 1 or 0.
	// store directly for later response verification
	op.ksafeValue = ksafeValue
	op.RequestParams = make(map[string]string)
	op.RequestParams["k"] = strconv.Itoa(ksafeValue)

	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

func (op *httpsMarkDesignKSafeOp) setupClusterHTTPRequest(hosts []string) error {
	// in practice, initiator only
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PutMethod
		httpRequest.buildHTTPSEndpoint("cluster/k-safety")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.QueryParams = op.RequestParams
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsMarkDesignKSafeOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsMarkDesignKSafeOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

// markDesignKSafeRsp will be either
// {"detail": "Marked design 0-safe"} OR
// {"detail": "Marked design 1-safe"}
type markDesignKSafeRsp struct {
	Detail string `json:"detail"`
}

func (op *httpsMarkDesignKSafeOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	// in practice, just the initiator node
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// The response object will be a dictionary, an example:
		// {"detail": "Marked design 0-safe"}
		markDesignKSafeResponse := markDesignKSafeRsp{}
		err := op.parseAndCheckResponse(host, result.content, &markDesignKSafeResponse)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// retrieve and verify the mark ksafety response
		var ksafeValue int
		if markDesignKSafeResponse.Detail == zeroSafeRspStr {
			ksafeValue = 0
		} else if markDesignKSafeResponse.Detail == oneSafeRspStr {
			ksafeValue = 1
		} else {
			err = fmt.Errorf(`[%s] fail to parse the ksafety value information, detail: %s`,
				op.name, markDesignKSafeResponse.Detail)
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// compare the ksafety_value from output JSON with k_value input value
		// verify if the return ksafety_value is the one we insert into the endpoint
		if ksafeValue != op.ksafeValue {
			err = fmt.Errorf(`[%s] mismatch between request and response k-safety values, request: %d, response: %d`,
				op.name, op.ksafeValue, ksafeValue)
			allErrs = errors.Join(allErrs, err)
			continue
		}

		op.logger.PrintInfo(`[%s] The K-safety value of the database is set as %d`, op.name, ksafeValue)
	}

	return allErrs
}

func (op *httpsMarkDesignKSafeOp) finalize(_ *opEngineExecContext) error {
	return nil
}
