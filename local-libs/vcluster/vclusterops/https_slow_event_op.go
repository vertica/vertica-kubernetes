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
	"io"
	"os"
	"strings"
)

type httpsSlowEventsOp struct {
	opBase
	transactionID string
	startTime     string
	endTime       string
	threadID      string
	nodeName      string
	phaseDuration string
	eventDesc     string
	// when debug mode is on, this op will return stub data
	debug bool
}

func makeHTTPSSlowEventOp(upHosts []string, startTime, endTime, transactionID, nodeName,
	threadID, phaseDuration, eventDesc string, debug bool) httpsSlowEventsOp {
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
	op.debug = debug
	return op
}

const (
	slowEventsURL = "dc/slow-events"
)

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *httpsSlowEventsOp) setupClusterHTTPRequest(hosts []string) error {
	// this op may consume resources of the database,
	// thus we only need to send https request to one of the up hosts

	// compose url from options
	url := slowEventsURL
	queryParams := []string{}

	if op.startTime != "" {
		queryParams = append(queryParams, "start-time="+op.startTime)
	}
	if op.endTime != "" {
		queryParams = append(queryParams, "end-time="+op.endTime)
	}
	if op.debug {
		queryParams = append(queryParams, "debug=true")
	}
	if op.nodeName != "" {
		queryParams = append(queryParams, "node-name="+op.nodeName)
	}
	if op.threadID != "" {
		queryParams = append(queryParams, "thread-id="+op.threadID)
	}
	if op.phaseDuration != "" {
		queryParams = append(queryParams, "phases-duration-desc"+op.phaseDuration)
	}
	if op.eventDesc != "" {
		queryParams = append(queryParams, "event-desc="+op.eventDesc)
	}

	for i, param := range queryParams {
		// replace " " with "%20" in query params
		queryParams[i] = strings.ReplaceAll(param, " ", "%20")
	}
	url += "?" + strings.Join(queryParams, "&")

	for _, host := range hosts[:1] {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint(url)
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsSlowEventsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsSlowEventsOp) execute(execContext *opEngineExecContext) error {
	if op.debug {
		return op.executeOnStub(execContext)
	}

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
			execContext.slowEvents = &slowEvents
			return allErrs
		}
	}

	return allErrs
}

func (op *httpsSlowEventsOp) executeOnStub(execContext *opEngineExecContext) error {
	// TODO: we take this location from input, but this place would be fine
	// because we can any way write files from outside to the test containers
	location := "/opt/vertica/tmp/slow_events_sample.json"
	jsonFile, err := os.Open(location)
	if err != nil {
		return fmt.Errorf("failed to open slow events stub file at %s", location)
	}

	defer jsonFile.Close()

	var slowEventList []dcSlowEvent
	bytes, _ := io.ReadAll(jsonFile)
	err = json.Unmarshal(bytes, &slowEventList)
	if err != nil {
		return err
	}

	var filteredEvents []dcSlowEvent
	for idx := range slowEventList {
		event := slowEventList[idx]
		if op.startTime != "" {
			if event.Time < op.startTime {
				continue
			}
		}
		if op.endTime != "" {
			if event.Time > op.endTime {
				continue
			}
		}
		if op.threadID != "" {
			if event.ThreadID != op.threadID {
				continue
			}
		}
		if op.eventDesc != "" {
			if !strings.Contains(event.EventDescription, op.eventDesc) {
				continue
			}
		}
		filteredEvents = append(filteredEvents, event)
	}

	execContext.slowEvents = new(dcSlowEvents)
	execContext.slowEvents.SlowEventList = filteredEvents

	return nil
}
