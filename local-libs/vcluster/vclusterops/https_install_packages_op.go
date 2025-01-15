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

type httpsInstallPackagesOp struct {
	opBase
	opHTTPSBase
	verbose        bool // Include verbose output about package install status
	forceReinstall bool
	status         InstallPackageStatus // Filled in once the op completes
}

func makeHTTPSInstallPackagesOp(hosts []string, useHTTPPassword bool,
	userName string, httpsPassword *string, forceReinstall bool, verbose bool,
) (httpsInstallPackagesOp, error) {
	op := httpsInstallPackagesOp{}
	op.name = "HTTPSInstallPackagesOp"
	op.description = "Install packages"
	op.hosts = hosts
	op.verbose = verbose
	op.forceReinstall = forceReinstall

	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.useHTTPPassword = useHTTPPassword
	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

func (op *httpsInstallPackagesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint("packages")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.QueryParams = map[string]string{
			"force-install": strconv.FormatBool(op.forceReinstall),
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsInstallPackagesOp) prepare(execContext *opEngineExecContext) error {
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

func (op *httpsInstallPackagesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsInstallPackagesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

/*
The response from the package endpoint, which are encoded in the next two
structs, will look like this:

{'packages': [

	             {
	               'package_name': 'ComplexTypes',
	               'install_status': 'skipped'
	             },
	             {
	               'package_name': 'DelimitedExport',
	               'install_status': 'skipped'
	             },
	           ...
	           ]
}
*/

// InstallPackageStatus provides status for each package install attempted.
type InstallPackageStatus struct {
	Packages []PackageStatus `json:"packages"`
}

// PackageStatus has install status for a single package.
type PackageStatus struct {
	// Name of the package this status is for
	PackageName string `json:"package_name"`
	// One word outcome of the install status:
	// Skipped, Success or Failure
	InstallStatus string `json:"install_status"`
}

func (op *httpsInstallPackagesOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

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

		if len(op.status.Packages) == 0 {
			err = fmt.Errorf(`[%s] response does not have status for any packages`, op.name)
			allErrs = errors.Join(allErrs, err)
		}

		// Only print out status if verbose output was requested. Otherwise,
		// just write status to the log.
		msg := fmt.Sprintf("[%s] installation status of packages: %v", op.name, op.status.Packages)
		if op.verbose {
			op.logger.PrintInfo(msg)
		} else {
			op.logger.V(1).Info(msg)
		}
	}
	return allErrs
}
