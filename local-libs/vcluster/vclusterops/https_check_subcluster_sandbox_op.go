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

type httpsCheckSubclusterSandboxOp struct {
	opBase
	opHTTPSBase
	ScToSandbox string
	Sandbox     string
}

// makeHTTPSCheckSubclusterSandboxOp initializes an op to find
// all subclusters and record their sandboxing information.
func makeHTTPSCheckSubclusterSandboxOp(hosts []string, scName string, sandbox string,
	useHTTPPassword bool, userName string, httpsPassword *string) (httpsCheckSubclusterSandboxOp, error) {
	op := httpsCheckSubclusterSandboxOp{}
	op.name = "HTTPSCheckSubclusterSandboxOp"
	op.description = "Find all subclusters and record their sandboxing information"
	op.hosts = hosts
	op.ScToSandbox = scName
	op.Sandbox = sandbox

	err := op.validateAndSetUsernameAndPassword(op.name, useHTTPPassword, userName,
		httpsPassword)

	return op, err
}

func (op *httpsCheckSubclusterSandboxOp) setupClusterHTTPRequest(hosts []string) error {
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

func (op *httpsCheckSubclusterSandboxOp) prepare(execContext *opEngineExecContext) error {
	if execContext.computeHosts != nil {
		op.hosts = util.SliceDiff(op.hosts, execContext.computeHosts)
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsCheckSubclusterSandboxOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

// the following struct will store a subcluster's information for this op
type subclusterSandboxInfo struct {
	SCName  string `json:"subcluster_name"`
	Sandbox string `json:"sandbox"`
}

type scResps struct {
	SCInfoList []subclusterSandboxInfo `json:"subcluster_list"`
}

func (op *httpsCheckSubclusterSandboxOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	keysToRemove := make(map[string]struct{})
	existingSandboxedHosts := make(map[string]string)
	mainClusterHosts := make(map[string]string)

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
		subclusterResp := scResps{}
		err := op.parseAndCheckResponse(host, result.content, &subclusterResp)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			return allErrs
		}

		// Process sandboxing info
		for _, scInfo := range subclusterResp.SCInfoList {
			mainHosts, sandboxedHosts, removeHosts := op.processScInfo(scInfo, execContext)
			// Accumulate maincluster hosts, hosts to be removed
			// and hosts that are sandboxed
			for host, sb := range mainHosts {
				mainClusterHosts[host] = sb
			}
			for h := range removeHosts {
				keysToRemove[h] = struct{}{}
			}
			for h, sb := range sandboxedHosts {
				existingSandboxedHosts[h] = sb
			}
		}
	}

	// Use updated scInfo
	for host, sb := range existingSandboxedHosts {
		// Just need one up host from the existing sandbox
		// This will be used to add new subcluster to an existing sandbox
		execContext.upHostsToSandboxes[host] = sb
		break
	}

	for host, sb := range mainClusterHosts {
		if _, exists := keysToRemove[host]; !exists {
			// Just one up host from main cluster
			execContext.upHostsToSandboxes[host] = sb
			break
		}
	}
	return allErrs
}
func (op *httpsCheckSubclusterSandboxOp) processScInfo(scInfo subclusterSandboxInfo,
	execContext *opEngineExecContext) (mainClusterHosts, existingSandboxedHosts map[string]string, keysToRemove map[string]struct{}) {
	keysToRemove = make(map[string]struct{})
	mainClusterHosts = make(map[string]string)
	for host, sc := range execContext.upScInfo {
		if scInfo.Sandbox != "" && scInfo.SCName == sc {
			keysToRemove, existingSandboxedHosts = op.processSandboxedSCInfo(scInfo, sc, host)
		} else {
			if scInfo.SCName == sc {
				mainClusterHosts[host] = scInfo.Sandbox
			}
			// We do not want a host from the sc to be sandboxed to be the initiator
			if sc == op.ScToSandbox {
				keysToRemove[host] = struct{}{}
			}
		}
	}
	return
}

func (op *httpsCheckSubclusterSandboxOp) processSandboxedSCInfo(scInfo subclusterSandboxInfo,
	sc, host string) (keysToRemove map[string]struct{}, existingSandboxedHosts map[string]string) {
	keysToRemove = make(map[string]struct{})
	existingSandboxedHosts = make(map[string]string)
	if scInfo.Sandbox != op.Sandbox {
		op.logger.Info("subcluster " + sc + " is sandboxed")
		if scInfo.SCName == sc {
			keysToRemove[host] = struct{}{}
		}
	} else {
		existingSandboxedHosts[host] = scInfo.Sandbox
	}
	return
}

func (op *httpsCheckSubclusterSandboxOp) finalize(_ *opEngineExecContext) error {
	return nil
}
