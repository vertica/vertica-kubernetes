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

type httpsInstallLicenseOp struct {
	opBase
	opHTTPSBase
	LicenseFilePath string
}

// makeHTTPSInstallLicenseOp will make an op that call vertica-https service to install license for database
// this op is a global op, so it should only be sent to one host of the DB group
func makeHTTPSInstallLicenseOp(hosts []string, useHTTPPassword bool, userName string,
	httpsPassword *string, licenseFilePath string) (httpsInstallLicenseOp, error) {
	op := httpsInstallLicenseOp{}
	op.name = "HTTPSInstallLicenseOp"
	op.description = "Install license for database"
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
	op.LicenseFilePath = licenseFilePath
	return op, nil
}

func (op *httpsInstallLicenseOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PutMethod
		httpRequest.buildHTTPSEndpoint(util.LicenseEndpoint)
		httpRequest.QueryParams = map[string]string{"licenseFile": op.LicenseFilePath}
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsInstallLicenseOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsInstallLicenseOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsInstallLicenseOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	// should only send request to one host as upgrade license is a global op
	// using for-loop here for accommodating potential future cases for sandboxes
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong password/certificate for https service on host %s", op.name, host)
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// upgrade license succeeds
		// the successful response object looks like the following:
		/* {
		   "detail":"Success: Replacing vertica license:
		             CompanyName: Vertica Systems, Inc.
		             start_date: YYYY-MM-DD
		             end_date: YYYY-MM-DD
		             grace_period: 0
		             capacity: Unlimited
		             Node Limit: Unlimited
		            "
		   }
		*/
		_, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			return fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
		}
		// upgrade succeeds, return now
		return nil
	}
	return allErrs
}

func (op *httpsInstallLicenseOp) finalize(_ *opEngineExecContext) error {
	return nil
}
