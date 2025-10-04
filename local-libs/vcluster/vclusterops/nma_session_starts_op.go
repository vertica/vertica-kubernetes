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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaSessionStartsOp struct {
	opBase
	hostRequestBodyMap map[string]string
	sessionID          string
	startTime          string
	endTime            string
	isDebug            bool
}

type sessionStartsRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

const sessionStartsURL = "dc/session-starts"

func makeNMASessionStartsOp(upHosts []string, userName string, dbName string, password *string,
	sessionID, startTime, endTime string, isDebug bool) (nmaSessionStartsOp, error) {
	op := nmaSessionStartsOp{}
	op.name = "NMASessionStartsOp"
	op.description = "Check Session Starts"
	op.hosts = upHosts[:1] // set up the request for one of the up hosts only
	op.sessionID = sessionID
	op.startTime = startTime
	op.endTime = endTime
	op.isDebug = isDebug

	// NMA endpoints don't need to differentiate between empty password and no password
	useDBPassword := password != nil
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, userName, password, dbName)
	if err != nil {
		return op, err
	}
	err = op.setupRequestBody(userName, dbName, useDBPassword, password)
	return op, err
}

func (op *nmaSessionStartsOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := sessionStartsRequestData{}

		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
		requestData.Params = make(map[string]any)
		if op.startTime != "" {
			requestData.Params["start-time"] = op.startTime
		}
		if op.endTime != "" {
			requestData.Params["end-time"] = op.endTime
		}
		if op.sessionID != "" {
			requestData.Params["session-id"] = op.sessionID
		}
		if op.isDebug {
			requestData.Params["debug"] = util.TrueStr
		} else {
			requestData.Params["debug"] = util.FalseStr
		}

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaSessionStartsOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint(sessionStartsURL)
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaSessionStartsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.opBase = op.opBase
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaSessionStartsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaSessionStartsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type dcSessionStarts struct {
	Time                     string `json:"time"`
	NodeName                 string `json:"node_name"`
	SessionID                string `json:"session_id"`
	UserID                   string `json:"user_id"`
	UserName                 string `json:"user_name"`
	ClientHostname           string `json:"client_hostname"`
	ClientPID                string `json:"client_pid"`
	ClientLabel              string `json:"client_label"`
	ClientType               string `json:"client_type"`
	ClientVersion            string `json:"client_version"`
	ClientOS                 string `json:"client_os"`
	ClientOSUserName         string `json:"client_os_user_name"`
	ClientOSHostname         string `json:"client_os_hostname"`
	SSLState                 string `json:"ssl_state"`
	TLSVersion               string `json:"tls_version"`
	SSLClientSubject         string `json:"ssl_client_subject"`
	SSLClientFingerprint     string `json:"ssl_client_fingerprint"`
	SSLCASubject             string `json:"ssl_ca_subject"`
	SSLCAFingerPrint         string `json:"ssl_ca_fingerprint"`
	AuthenticationMethod     string `json:"authentication_method"`
	ClientAuthenticationName string `json:"client_authentication_name"`
	IsInternal               string `json:"is_internal"`
	RequestedProtocol        string `json:"requested_protocol"`
	EffectiveProtocol        string `json:"effective_protocol"`
	SessionType              string `json:"session_type"`
	IsBinaryTransfer         string `json:"is_binary_transfer"`
}

func (op *nmaSessionStartsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			var sessionStartList []dcSessionStarts
			err := op.parseAndCheckResponse(host, result.content, &sessionStartList)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			// we only need result from one host
			execContext.dcSessionStarts = &sessionStartList
			return allErrs
		}
	}

	return allErrs
}
