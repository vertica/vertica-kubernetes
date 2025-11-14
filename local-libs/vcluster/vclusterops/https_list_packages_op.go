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
	"strconv"

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsListPackagesOp struct {
	opBase
	opHTTPSBase
	packageFilter string
	checkStatus   bool
	status        ListPackageStatus // Filled in once the op completes
}

func makeHTTPSListPackagesOp(hosts []string, useHTTPPassword bool,
	userName string, httpsPassword *string, packageFilter string, checkStatus bool,
) (httpsListPackagesOp, error) {
	op := httpsListPackagesOp{}
	op.name = "HTTPSListPackagesOp"
	op.description = "List packages"
	op.hosts = hosts
	op.packageFilter = packageFilter
	op.checkStatus = checkStatus

	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.useHTTPPassword = useHTTPPassword
	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

func (op *httpsListPackagesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("packages")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.QueryParams = map[string]string{
			"check-status": strconv.FormatBool(op.checkStatus),
		}
		// Add filter parameter only if specified and not "all"
		// API defaults to "all" when filter is omitted
		if op.packageFilter != "" && op.packageFilter != FilterAll {
			httpRequest.QueryParams["filter"] = op.packageFilter
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsListPackagesOp) prepare(execContext *opEngineExecContext) error {
	// If no hosts passed in, we will find the hosts from execute-context
	if len(op.hosts) == 0 {
		if len(execContext.upHosts) == 0 {
			return fmt.Errorf(`[%s] Cannot find any up hosts in OpEngineExecContext`, op.name)
		}
		// use first up host to execute https post request
		op.hosts = []string{execContext.upHosts[0]}
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsListPackagesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsListPackagesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

/*
The response from the GET /packages endpoint will look like this:

{
  'packages': [
    {
      'package_name': 'ComplexTypes',
      'description': 'Complex data types library',
      'install_status': 'Yes',
	  'auto_install': true
    },
    {
      'package_name': 'DelimitedExport',
      'description': 'Export data in delimited formats',
      'install_status': 'No',
      'auto_install': false
    },
    ...
  ],
}
*/

// ListPackageStatus provides status for each package listed.
type ListPackageStatus struct {
	Packages []PackageDetail `json:"packages"`
}

// PackageDetail has the details for a single package.
type PackageDetail struct {
	PackageName   string `json:"package_name"`
	Description   string `json:"description"`
	InstallStatus string `json:"install_status"` // "Yes", "No", or unknown (offline mode)
	AutoInstall   bool   `json:"auto_install"`
}

func (op *httpsListPackagesOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	// List packages is cluster-wide - return after first successful response
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
