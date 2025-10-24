/*
 (c) Copyright [2023-2025] Open Text.
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

type ShutdownType string

const (
	nmaShutdownOpMsgKey     = "shutdown_message"
	nmaShutdownOpErrorKey   = "shutdown_error"
	nmaShutdownOpMsgSuccess = "NMA server stopped"
	nmaShutdownOpMsgFail    = "NMA server shutdown failed"

	restart  ShutdownType = "restart"
	shutdown ShutdownType = "shutdown"
)

type nmaShutdownOp struct {
	opBase
	shutdownType ShutdownType
}

func makeNMARestartOp(hosts []string) nmaShutdownOp {
	op := nmaShutdownOp{}
	op.name = "NMARestartOp"
	op.description = "Restart NMA service"
	op.hosts = hosts
	op.shutdownType = restart
	return op
}

func makeNMAShutdownOp(hosts []string) nmaShutdownOp {
	op := nmaShutdownOp{}
	op.name = "NMAShutdownOp"
	op.description = "Shut down NMA service"
	op.hosts = hosts
	op.shutdownType = shutdown
	return op
}

func (op *nmaShutdownOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PutMethod
		httpRequest.buildNMAEndpoint("nma/" + string(op.shutdownType))
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}
func (op *nmaShutdownOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaShutdownOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaShutdownOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaShutdownOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			// the response object will be a map as follows:
			// {
			//     'shutdown_scheduled':  'NMA server shutdown scheduled',
			//     'shutdown_message':  'NMA server stopped',
			//     'shutdown_error':  'Null'
			// }
			//
			// OR
			//
			// {
			//     'shutdown_scheduled':  'NMA server shutdown scheduled',
			//     'shutdown_message':  'NMA server shutdown failed',
			//     'shutdown_error':  '<errmsg>'
			// }
			//
			// OR
			//
			// {
			//     'restart_scheduled':  'NMA server restart scheduled',
			//     'shutdown_message':  'NMA server stopped',
			//     'shutdown_error':  'Null'
			// }
			//
			// OR
			//
			// {
			//     'restart_scheduled':  'NMA server restart scheduled',
			//     'shutdown_message':  'NMA server shutdown failed',
			//     'shutdown_error':  '<errmsg>'
			// }
			responseObj, err := op.parseAndCheckMapResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}

			shutdownMsg, ok := responseObj[nmaShutdownOpMsgKey]
			if !ok {
				err = op.makeMissingFieldError(nmaShutdownOpMsgKey)
				allErrs = errors.Join(allErrs, err)
				continue
			}
			if shutdownMsg == nmaShutdownOpMsgSuccess {
				continue
			}
			shutdownErrMsg, ok := responseObj[nmaShutdownOpErrorKey]
			if !ok {
				err = op.makeMissingFieldError(nmaShutdownOpErrorKey)
				allErrs = errors.Join(allErrs, err)
				continue
			}
			allErrs = errors.Join(allErrs, fmt.Errorf("nma "+string(op.shutdownType)+" failed, details: %s", shutdownErrMsg))
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}

func (op *nmaShutdownOp) makeMissingFieldError(field string) error {
	return fmt.Errorf(`[%s] response does not contain field "%s"`, op.name, field)
}
