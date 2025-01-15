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
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type httpsGetSystemTablesOp struct {
	opBase
	opHTTPSBase
}

func makeHTTPSGetSystemTablesOp(logger vlog.Printer, hosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string,
) (httpsGetSystemTablesOp, error) {
	op := httpsGetSystemTablesOp{}
	op.name = "HTTPSGetSystemTablesOp"
	op.description = "Collect system tables information"
	op.logger = logger.WithName(op.name)
	op.hosts = hosts
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

func (op *httpsGetSystemTablesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("system-tables")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsGetSystemTablesOp) prepare(execContext *opEngineExecContext) error {
	host := getInitiatorFromUpHosts(execContext.upHosts, op.hosts)
	if host == "" {
		op.logger.PrintWarning("no up hosts among user specified hosts to collect system tables from, skipping the operation")
		op.skipExecute = true
		return nil
	}

	// construct host list for interface purposes
	op.hosts = []string{host}

	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsGetSystemTablesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

type systemTableInfo struct {
	TableName string `json:"table_name"`
	Schema    string `json:"schema"`
}

type systemTableListInfo struct {
	SystemTableList []systemTableInfo `json:"system_table_list"`
}

func (op *httpsGetSystemTablesOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong password/certificate for https service on host %s",
				op.name, host)
		}

		if result.isPassing() {
			// unmarshal the response content
			systemTableList := systemTableListInfo{}
			err := op.parseAndCheckResponse(host, result.content, &systemTableList)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				return appendHTTPSFailureError(allErrs)
			}

			execContext.systemTableList = systemTableList

			return nil
		}
		allErrs = errors.Join(allErrs, result.err)
	}
	return appendHTTPSFailureError(allErrs)
}

func (op *httpsGetSystemTablesOp) finalize(_ *opEngineExecContext) error {
	return nil
}
