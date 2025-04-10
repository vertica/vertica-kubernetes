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

type httpsSlowEventsOp struct {
	opBase
	startTime     string
	endTime       string
	threadID      string
	phaseDuration string
	transactionID string
	nodeName      string
	eventDesc     string
}

func makeHTTPSSlowEventOp(upHosts []string,
	startTime, endTime, threadID, phaseDuration string,
	transactionID, nodeName, eventDesc string) httpsSlowEventsOp {
	op := httpsSlowEventsOp{}
	op.name = "HTTPSSlowEventOp"
	op.description = "Check slow events"
	op.hosts = upHosts
	op.startTime = startTime
	op.endTime = endTime
	op.transactionID = transactionID
	op.nodeName = nodeName
	op.threadID = threadID
	op.phaseDuration = phaseDuration
	op.eventDesc = eventDesc
	return op
}

func makeHTTPSSlowEventOpByThreadID(upHosts []string,
	startTime, endTime, threadID string) httpsSlowEventsOp {
	return makeHTTPSSlowEventOp(upHosts, startTime, endTime, threadID, "", "", "", "")
}

func makeHTTPSSlowEventOpByKeyword(upHosts []string,
	startTime, endTime, keyword string) httpsSlowEventsOp {
	return makeHTTPSSlowEventOp(upHosts, startTime, endTime, "", keyword, "", "", "")
}

const (
	slowEventsURL = "dc/slow-events"
)

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *httpsSlowEventsOp) setupClusterHTTPRequest(hosts []string) error {
	// this op may consume resources of the database,
	// thus we only need to send https request to one of the up hosts

	// compose url from options
	baseURL := slowEventsURL

	// set up the request for one of the up hosts only
	for _, host := range hosts[:1] {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		queryParams := make(map[string]string)
		if op.nodeName != "" {
			queryParams["node-name"] = op.nodeName
		}
		if op.startTime != "" {
			queryParams["start-time"] = op.startTime
		}
		if op.endTime != "" {
			queryParams["end-time"] = op.endTime
		}
		if op.threadID != "" {
			queryParams["thread-id"] = op.threadID
		}
		if op.phaseDuration != "" {
			queryParams["phases-duration-desc"] = op.phaseDuration
		}
		if op.eventDesc != "" {
			queryParams["event-desc"] = op.eventDesc
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

func (op *httpsSlowEventsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsSlowEventsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsSlowEventsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type dcSlowEvents struct {
	SlowEventList []dcSlowEvent `json:"dc_slow_event_list"`
}

type dcSlowEvent struct {
	Time             string `json:"timestamp"`
	NodeName         string `json:"node_name"`
	SessionID        string `json:"session_id"`
	UserID           string `json:"user_id"`
	UserName         string `json:"user_name"`
	TxnID            string `json:"txn_id"`
	StatementID      string `json:"statement_id"`
	RequestID        string `json:"request_id"`
	EventDescription string `json:"event_description"`
	ThresholdUs      int64  `json:"threshold_us"`
	DurationUs       int64  `json:"duration_us"`
	PhasesDurationUs string `json:"phases_duration_us"`
	ThreadID         string `json:"thread_id"`
	Val3             string `json:"val3"`
}

func (op *httpsSlowEventsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		if result.isPassing() {
			var slowEvents dcSlowEvents
			err := op.parseAndCheckResponse(host, result.content, &slowEvents)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			// we only need result from one host
			execContext.dcSlowEvents = &slowEvents
			return allErrs
		}
	}

	return allErrs
}
