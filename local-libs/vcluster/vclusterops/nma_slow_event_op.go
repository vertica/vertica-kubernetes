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

type nmaSlowEventsOp struct {
	opBase
	hostRequestBodyMap map[string]string
	startTime          string
	endTime            string
	threadID           string
	phasesDuration     string
	transactionID      string
	nodeName           string
	eventDesc          string
	durationUs         string
	isDebug            bool
}

type slowEventRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

func makeNMASlowEventOp(upHosts []string, userName string,
	dbName string, password *string,
	startTime, endTime, threadID, phaseDuration string,
	transactionID, nodeName, eventDesc, durationUs string, isDebug bool) (nmaSlowEventsOp, error) {
	op := nmaSlowEventsOp{}
	op.name = "NMASlowEventOp"
	op.description = "Check slow events"
	op.hosts = upHosts[:1] // set up the request for one of the up hosts only
	op.startTime = startTime
	op.endTime = endTime
	op.transactionID = transactionID
	op.nodeName = nodeName
	op.threadID = threadID
	op.phasesDuration = phaseDuration
	op.eventDesc = eventDesc
	op.durationUs = durationUs
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

func (op *nmaSlowEventsOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := slowEventRequestData{}

		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
		requestData.Params = make(map[string]any)
		// TODO: the endpoint validator should tolerate empty input
		if op.startTime != "" {
			requestData.Params["start-time"] = op.startTime
		}
		if op.endTime != "" {
			requestData.Params["end-time"] = op.endTime
		}
		if op.isDebug {
			requestData.Params["debug"] = util.TrueStr
		} else {
			requestData.Params["debug"] = util.FalseStr
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
		if op.durationUs != "" {
			requestData.Params["duration-us"] = op.durationUs
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
	execContext.dispatcher.opBase = op.opBase
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
	Time             string               `json:"time"`
	NodeName         string               `json:"node_name"`
	SessionID        string               `json:"session_id"`
	UserID           string               `json:"user_id"`
	UserName         string               `json:"user_name"`
	TxnID            string               `json:"transaction_id"`
	StatementID      string               `json:"statement_id"`
	RequestID        string               `json:"request_id"`
	EventDescription string               `json:"event_description"`
	ThresholdUs      string               `json:"threshold_us"`
	DurationUs       string               `json:"duration_us"`
	PhasesDurationUs string               `json:"phases_duration_us"`
	ThreadID         string               `json:"thread_id"`
	Val3             string               `json:"val3"`
	SessionInfo      *dcSessionStarts     `json:"session_info"`
	TxnInfo          *dcTransactionStarts `json:"transaction_info"`
}

func (event *dcSlowEvent) getSessionID() string {
	return event.SessionID
}

func (event *dcSlowEvent) getTxnID() string {
	return event.TxnID
}

func (event *dcSlowEvent) getThreadID() string {
	return event.ThreadID
}

func (op *nmaSlowEventsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
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
