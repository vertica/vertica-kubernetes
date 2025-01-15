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

type nmaNetworkProfileOp struct {
	opBase
}

func makeNMANetworkProfileOp(hosts []string) nmaNetworkProfileOp {
	op := nmaNetworkProfileOp{}
	op.name = "NMANetworkProfileOp"
	op.description = "Get network profile of cluster"
	op.hosts = hosts
	return op
}

func (op *nmaNetworkProfileOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("network-profiles")
		httpRequest.QueryParams = map[string]string{"broadcast-hint": host}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaNetworkProfileOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaNetworkProfileOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaNetworkProfileOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type networkProfile struct {
	Name      string
	Address   string
	Subnet    string
	Netmask   string
	Broadcast string
}

func (op *nmaNetworkProfileOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error

	allNetProfiles := make(map[string]networkProfile)

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			// unmarshal the result content
			profile, err := op.parseResponse(host, result.content)
			if err != nil {
				return fmt.Errorf("[%s] fail to parse network profile on host %s, details: %w",
					op.name, host, err)
			}
			allNetProfiles[host] = profile
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	// save network profiles to execContext
	execContext.networkProfiles = allNetProfiles

	return allErrs
}

func (op *nmaNetworkProfileOp) parseResponse(host, resultContent string) (networkProfile, error) {
	var responseObj networkProfile

	// the response_obj will be a dictionary like the following:
	// {
	//   "name" : "eth0",
	//   "address" : "192.168.100.1",
	//   "subnet" : "192.168.0.0/16"
	//   "netmask" : "255.255.0.0"
	//   "broadcast": "192.168.255.255"
	// }
	err := op.parseAndCheckResponse(host, resultContent, &responseObj)
	if err != nil {
		return responseObj, err
	}

	// check whether any field is empty
	err = util.CheckMissingFields(responseObj)

	return responseObj, err
}
