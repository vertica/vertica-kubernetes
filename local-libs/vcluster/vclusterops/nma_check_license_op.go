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
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type nmaCheckLicenseOp struct {
	opBase
	hostRequestBody     string
	initiator           string
	ceLicenseDisallowed bool
	logger              vlog.Printer
}

// http request model
type checkLicenseData struct {
	sqlEndpointData
	LicenseFile string `json:"license_file"`
}

type CheckLicenseResponse map[string]string

func makeNMACheckLicenseOp(hosts []string, username, dbName, licenseFile string, password *string, useHTTPPassword bool,
	ceLicenseDisallowed bool, logger vlog.Printer) (nmaCheckLicenseOp, error) {
	op := nmaCheckLicenseOp{}
	op.name = "NMACheckLicenseOp"
	op.description = "Check license"
	op.hosts = hosts
	op.logger = logger
	op.ceLicenseDisallowed = ceLicenseDisallowed
	err := op.setupRequestBody(username, dbName, licenseFile, password, useHTTPPassword)
	if err != nil {
		return op, err
	}
	return op, nil
}

func (op *nmaCheckLicenseOp) setupRequestBody(
	username, dbName, licenseFile string, password *string,
	useDBPassword bool) error {
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	checkLicenseData := &checkLicenseData{}
	checkLicenseData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	checkLicenseData.LicenseFile = licenseFile
	dataBytes, err := json.Marshal(checkLicenseData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}
	op.hostRequestBody = string(dataBytes)

	checkLicenseData.LicenseFile = "******"
	maskedDataBytes, err := json.Marshal(checkLicenseData)
	if err != nil {
		return nil
	}
	op.logger.Info("request data", "op name", op.name, "hostRequestBody", string(maskedDataBytes))
	return nil
}

func (op *nmaCheckLicenseOp) setupClusterHTTPRequest(initiator string) error {
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PostMethod
	httpRequest.buildNMAEndpoint("vertica/license-check")
	httpRequest.RequestData = op.hostRequestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest

	return nil
}

func (op *nmaCheckLicenseOp) prepare(execContext *opEngineExecContext) error {
	// select an up host in the sandbox or main cluster as the initiator
	initiator, err := getInitiatorHost(op.hosts, []string{})
	if err != nil {
		return err
	}
	op.initiator = initiator
	execContext.dispatcher.setup([]string{op.initiator})
	return op.setupClusterHTTPRequest(op.initiator)
}

func (op *nmaCheckLicenseOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	err := op.processResult(execContext)
	if err != nil {
		return err
	}
	op.logger.Info("Vertica License has been valicated successfully")
	return nil
}

func (op *nmaCheckLicenseOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaCheckLicenseOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			op.logger.Info("Check license rest call is a success", "response", result.content)
			checkLicenseResponse := CheckLicenseResponse{}
			err := json.Unmarshal([]byte(result.content), &checkLicenseResponse)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			} else if op.ceLicenseDisallowed {
				companyName, ok := checkLicenseResponse["company_name"]
				if ok {
					companyName = strings.Trim(companyName, " ")
					if companyName == "Vertica Community Edition" {
						allErrs = errors.Join(allErrs, fmt.Errorf("vertica Community Edition license has been disallowed"))
					}
				}
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}
	return allErrs
}
