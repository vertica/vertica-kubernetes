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

const HTTPSSuccMsg = "REBALANCED SHARDS"

type httpsRebalanceSubclusterShardsOp struct {
	opBase
	opHTTPSBase
	scName string
}

// makeHTTPSRebalanceSubclusterShardsOp creates an op that calls vertica-http service to rebalance shards of a subcluster
func makeHTTPSRebalanceSubclusterShardsOp(bootstrapHost []string, useHTTPPassword bool, userName string,
	httpsPassword *string, scName string) (httpsRebalanceSubclusterShardsOp, error) {
	op := httpsRebalanceSubclusterShardsOp{}
	op.name = "HTTPSRebalanceSubclusterShardsOp"
	op.description = "Initiate rebalance of subcluster shards"
	op.hosts = bootstrapHost
	op.scName = scName

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

func (op *httpsRebalanceSubclusterShardsOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.SubclustersEndpoint + op.scName + "/rebalance")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsRebalanceSubclusterShardsOp) prepare(execContext *opEngineExecContext) error {
	// rebalance shards on the default subcluster if scName isn't provided
	if op.scName == "" {
		if execContext.defaultSCName == "" {
			return errors.New("default subcluster is not set")
		}
		op.scName = execContext.defaultSCName
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsRebalanceSubclusterShardsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsRebalanceSubclusterShardsOp) processResult(_ *opEngineExecContext) error {
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
		// The successful response object will be a dictionary:
		/*
			{
			  "detail": "REBALANCED SHARDS"
			}
		*/
		resp, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			return allErrs
		}
		// verify if the response's content is correct
		if resp["detail"] != HTTPSSuccMsg {
			err = fmt.Errorf(`[%s] response detail should be '%s' but got '%s'`, op.name, HTTPSSuccMsg, resp["detail"])
			allErrs = errors.Join(allErrs, err)
			return allErrs
		}

		return nil
	}

	return allErrs
}

func (op *httpsRebalanceSubclusterShardsOp) finalize(_ *opEngineExecContext) error {
	return nil
}
