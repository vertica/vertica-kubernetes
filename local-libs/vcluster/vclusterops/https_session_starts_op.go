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
	"net/url"
	"strings"
)

type httpsSessionStartsOp struct {
	opBase

	sessionID string
	startTime string
	endTime   string
}

func makeHTTPSSessionStartsOp(upHosts []string, sessionID, startTime, endTime string) httpsSessionStartsOp {
	op := httpsSessionStartsOp{}
	op.name = "HTTPSSessionStartsOp"
	op.description = "Check Session Starts"
	op.hosts = upHosts
	op.sessionID = sessionID
	op.startTime = startTime
	op.endTime = endTime
	return op
}

const sessionStartsURL = "dc/session-starts"

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *httpsSessionStartsOp) setupClusterHTTPRequest(hosts []string) error {
	// this op may consume resources of the database,
	// thus we only need to send https request to one of the up hosts
	baseURL := sessionStartsURL
	for _, host := range hosts[:1] {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		queryParams := make(map[string]string)
		if op.sessionID != "" {
			queryParams["session-id"] = op.sessionID
		}
		if op.startTime != "" {
			queryParams["start-time"] = op.startTime
		}
		if op.endTime != "" {
			queryParams["end-time"] = op.endTime
		}

		// Build query string
		var queryParts []string
		for key, value := range queryParams {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", key, value))
		}
		// We use string concatenation to build the url to avoid query param encoding of the timestamp fields
		// Join query parts to form a query string
		queryString := url.PathEscape(strings.Join(queryParts, "&"))
		httpRequest.buildHTTPSEndpoint(fmt.Sprintf("%s?%s", baseURL, queryString))

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsSessionStartsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetive and confusing information.
	op.spinner = nil

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
			execContext.dcSessionStarts = &sessionStarts
			return allErrs
		}
	}

	return allErrs
}
