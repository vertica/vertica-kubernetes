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
	"strconv"
	"strings"
)

const (
	startTimeParam = "start-time="
	endTimeParam   = "end-time="
	sessionIDParam = "session-id="
	txnIDParam     = "txn-id="
	debugParam     = "debug="
)

type httpsSessionStartsOp struct {
	opBase

	// when debug mode is on, this op will return stub data
	sessionID string
	startTime string
	endTime   string
	debug     bool
}

func makeHTTPSSessionStartsOp(upHosts []string, sessionID, startTime, endTime string, debug bool) httpsSessionStartsOp {
	op := httpsSessionStartsOp{}
	op.name = "HTTPSSessionStartsOp"
	op.description = "Check Session Starts"
	op.hosts = upHosts
	op.sessionID = sessionID
	op.startTime = startTime
	op.endTime = endTime
	op.debug = debug
	return op
}

const sessionStartsURL = "dc/session-starts"

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *httpsSessionStartsOp) setupClusterHTTPRequest(hosts []string) error {
	// this op may consume resources of the database,
	// thus we only need to send https request to one of the up hosts
	url := sessionStartsURL
	queryParams := []string{}
	if op.sessionID != "" {
		queryParams = append(queryParams, sessionIDParam+op.sessionID)
	}
	if op.debug {
		queryParams = append(queryParams, debugParam+strconv.FormatBool(op.debug))
	}
	if op.startTime != "" {
		queryParams = append(queryParams, startTimeParam+op.startTime)
	}
	if op.endTime != "" {
		queryParams = append(queryParams, endTimeParam+op.endTime)
	}
	for i, param := range queryParams {
		// replace " " with "%20" in query params
		queryParams[i] = strings.ReplaceAll(param, " ", "%20")
	}

	if len(queryParams) > 0 {
		url += "?" + strings.Join(queryParams, "&")
	}
	for _, host := range hosts[:1] {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint(url)
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsSessionStartsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsSessionStartsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsSessionStartsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type dcSessionStarts struct {
	SessionStartsList []dcSessionStart `json:"dc_session_starts_list"`
}

type dcSessionStart struct {
	Time                     string `json:"timestamp"`
	NodeName                 string `json:"node_name"`
	SessionID                string `json:"session_id"`
	UserID                   int64  `json:"user_id"`
	UserName                 string `json:"user_name"`
	ClientHostname           string `json:"client_hostname"`
	ClientPID                int64  `json:"client_pid"`
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
	IsInternal               bool   `json:"is_internal"`
	RequestedProtocol        string `json:"requested_protocol"`
	EffectiveProtocol        string `json:"effective_protocol"`
	SessionType              string `json:"session_type"`
	IsBinaryTransfer         bool   `json:"is_binary_transfer"`
}

func (op *httpsSessionStartsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			var sessionStarts dcSessionStarts
			err := op.parseAndCheckResponse(host, result.content, &sessionStarts)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			// we only need result from one host
			execContext.dcSessionStarts = sessionStarts
			return allErrs
		}
	}

	return allErrs
}
