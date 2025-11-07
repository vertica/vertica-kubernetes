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

type httpsUninstallPackagesOp struct {
	opBase
	opHTTPSBase
	packageFilter string
	status        UninstallPackagesStatus // Filled in once the op completes
}

func makeHTTPSUninstallPackagesOp(hosts []string, useHTTPPassword bool,
	userName string, httpsPassword *string, packageFilter string,
) (httpsUninstallPackagesOp, error) {
	op := httpsUninstallPackagesOp{}
	op.name = "HTTPSUninstallPackagesOp"
	op.description = "Uninstall packages"
	op.hosts = hosts
	op.packageFilter = packageFilter

	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.useHTTPPassword = useHTTPPassword
	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

func (op *httpsUninstallPackagesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = DeleteMethod
		httpRequest.buildHTTPSEndpoint("packages")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		// Add filter parameter only if specified and not "all"
		// API defaults to "all" when filter is omitted
		if op.packageFilter != "" && op.packageFilter != util.PkgFilterAll {
			httpRequest.QueryParams = make(map[string]string)
			httpRequest.QueryParams["packages"] = op.packageFilter
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsUninstallPackagesOp) prepare(execContext *opEngineExecContext) error {
	// If no hosts passed in, we will find the hosts from execute-context
	if len(op.hosts) == 0 {
		if len(execContext.upHosts) == 0 {
			return fmt.Errorf(`[%s] Cannot find any up hosts in OpEngineExecContext`, op.name)
		}
		// use first up host to execute https delete request
		op.hosts = []string{execContext.upHosts[0]}
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsUninstallPackagesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsUninstallPackagesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

/*
The response from the DELETE /v1/packages endpoint will look like this:

{
  'packages': [
    {
      'package_name': 'ComplexTypes',
      'install_status': 'Uninstalled successfully',
    },
    {
      'package_name': 'pgcompat',
      'install_status': 'Skipped - not currently installed',
    },
	{
      'package_name': 'MachineLearning',
      'install_status': 'Failed - uninstall script error: VIAssert(pt) failed,
    },
    ...
  ],
}
*/

// UninstallPackagesStatus provides status for each package.
type UninstallPackagesStatus struct {
	Packages []UninstallPackageDetail `json:"packages"`
}

// UninstallPackageDetail has the details for a single package.
type UninstallPackageDetail struct {
	PackageName   string `json:"package_name"`
	InstallStatus string `json:"install_status"`
}

func (op *httpsUninstallPackagesOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	// Uninstall packages is cluster-wide - return after first successful response
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		err := op.parseAndCheckResponse(host, result.content, &op.status)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// Success! Return immediately, ignoring any previous errors.
		return nil
	}
	// Only return errors if ALL hosts failed
	return allErrs
}
