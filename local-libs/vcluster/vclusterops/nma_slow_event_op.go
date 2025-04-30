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
	"strconv"
)

type nmaSlowEventsOp struct {
	opBase
	hostRequestBodyMap map[string]string
	userName           string
	startTime          string
	endTime            string
	threadID           string
	phasesDuration     string
	transactionID      string
	nodeName           string
	eventDesc          string
}

type slowEventRequestData struct {
	Params   map[string]any `json:"params"`
	Username string         `json:"username"`
}

func makeNMASlowEventOp(upHosts []string, userName string,
	startTime, endTime, threadID, phaseDuration string,
	transactionID, nodeName, eventDesc string) (nmaSlowEventsOp, error) {
	op := nmaSlowEventsOp{}
	op.name = "NMASlowEventOp"
	op.description = "Check slow events"
	op.hosts = upHosts // set up the request for one of the up hosts only
	op.userName = userName
	op.startTime = startTime
	op.endTime = endTime
	op.transactionID = transactionID
	op.nodeName = nodeName
	op.threadID = threadID
	op.phasesDuration = phaseDuration
	op.eventDesc = eventDesc

	err := op.setupRequestBody()
	if err != nil {
		return op, err
	}

	return op, nil
}

func makeNMASlowEventOpByThreadID(upHosts []string, userName string,
	startTime, endTime, threadID string) (nmaSlowEventsOp, error) {
	return makeNMASlowEventOp(upHosts, userName, startTime, endTime, threadID, "", "", "", "")
}

func makeNMASlowEventOpByKeyword(upHosts []string, userName string,
	startTime, endTime, keyword string) (nmaSlowEventsOp, error) {
	return makeNMASlowEventOp(upHosts, userName, startTime, endTime, "", keyword, "", "", "")
}

func (op *nmaSlowEventsOp) setupRequestBody() error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := slowEventRequestData{}

		requestData.Username = op.userName
		requestData.Params = make(map[string]any)
		// TODO: the endpoint validator should tolerate empty input
		if op.startTime != "" {
			requestData.Params["start-time"] = op.startTime
		}
		if op.endTime != "" {
			requestData.Params["end-time"] = op.endTime
		}
		if op.transactionID != "" {
			requestData.Params["txn-id"] = op.transactionID
		}
		if op.nodeName != "" {
			requestData.Params["node-name"] = op.nodeName
		}
		if op.threadID != "" {
			requestData.Params["thread-id"] = op.threadID
		}
		if op.phasesDuration != "" {
			requestData.Params["phases-duration-us"] = op.phasesDuration
		}
		if op.eventDesc != "" {
			requestData.Params["event-desc"] = op.eventDesc
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
func (op *nmaSlowEventsOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("dc/slow-events")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaSlowEventsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaSlowEventsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaSlowEventsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type dcSlowEvent struct {
	// TODO: all the IDs should be casted into string
	Time             string `json:"time"`
	NodeName         string `json:"node_name"`
	SessionID        string `json:"session_id"`
	UserID           int64  `json:"user_id"`
	UserName         string `json:"user_name"`
	TxnID            int    `json:"txn_id"`
	StatementID      int64  `json:"statement_id"`
	RequestID        int64  `json:"request_id"`
	EventDescription string `json:"event_description"`
	ThresholdUs      int64  `json:"threshold_us"`
	DurationUs       int64  `json:"duration_us"`
	PhasesDurationUs string `json:"phases_duration_us"`
	ThreadID         int64  `json:"thread_id"`
	Val3             string `json:"val3"`
}

func (event *dcSlowEvent) getSessionID() string {
	return event.SessionID
}

func (event *dcSlowEvent) getTxnID() string {
	// TODO: make the TxnID into string
	return strconv.Itoa(event.TxnID)
}

func (op *nmaSlowEventsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		// for any passing result, directly return
		if result.isPassing() {
			var slowEventList []dcSlowEvent
			err := op.parseAndCheckResponse(host, result.content, &slowEventList)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			execContext.dcSlowEventList = &slowEventList
			return nil
		}

		// record the error in failed results
		allErrs = errors.Join(allErrs, result.err)
	}

	return allErrs
}
