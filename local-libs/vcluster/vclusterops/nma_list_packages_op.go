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

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaListPackagesOp struct {
	opBase
	packageFilter string
	hosts         []string
	packages      *[]PackageDetail
}

// makeNMAListPackagesOp creates an operation to list packages from NMA endpoint (offline mode)
func makeNMAListPackagesOp(hosts []string, packageFilter string, packages *[]PackageDetail) nmaListPackagesOp {
	op := nmaListPackagesOp{}
	op.name = "NMAListPackagesOp"
	op.description = "List packages from filesystem (offline mode)"
	op.hosts = hosts
	op.packageFilter = packageFilter
	op.packages = packages
	return op
}

func (op *nmaListPackagesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("packages")
		httpRequest.QueryParams = map[string]string{}

		// Add query parameters if filter is specified
		if op.packageFilter != "" && op.packageFilter != util.PkgFilterAll {
			httpRequest.QueryParams["filter"] = op.packageFilter
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaListPackagesOp) prepare(execContext *opEngineExecContext) error {
	if len(op.hosts) == 0 {
		return fmt.Errorf("[%s] no hosts provided", op.name)
	}

	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaListPackagesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaListPackagesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaListPackagesOp) processResult(_ *opEngineExecContext) error {
	op.logger.PrintInfo("[%s] processing list packages response", op.name)

	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			allErrs = errors.Join(allErrs, fmt.Errorf("[%s] wrong certificate for NMA service on host %s", op.name, host))
			continue
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// NMA endpoint returns wrapped format: {"packages": [...]}
		var responseObj struct {
			Packages []PackageDetail `json:"packages"`
		}

		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		op.logger.PrintInfo("[%s] successfully retrieved %d packages from host %s",
			op.name, len(responseObj.Packages), host)

		*op.packages = responseObj.Packages

		// Return immediately after first successful response
		return nil
	}

	return fmt.Errorf("[%s] failed to list packages from any host: %w", op.name, allErrs)
}
