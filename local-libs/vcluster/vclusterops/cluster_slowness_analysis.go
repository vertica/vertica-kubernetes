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
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type SlowEventNode struct {
	Depth           int                 `json:"depth"`
	Event           *dcSlowEvent        `json:"slow_event"`
	Session         *dcSessionStart     `json:"session"`
	Transaction     *dcTransactionStart `json:"transaction"`
	PriorHoldEvents []dcSlowEvent       `json:"prior_hold_events"`
	Leaf            bool                `json:"leaf"`
}

func (opt *VClusterHealthOptions) buildCascadeGraph(logger vlog.Printer, upHosts []string) error {
	// get slow events during the given time
	slowEvents, err := opt.getSlowEvents(logger, upHosts,
		"" /*thread_id*/, opt.StartTime, opt.EndTime, true /*for cascade*/)
	if err != nil {
		return err
	}

	// find the slowest event during the given time
	if slowEvents == nil || len(slowEvents.SlowEventList) == 0 {
		return nil
	}

	vlog.DisplayColorInfo("Building cascade graph for slow events")

	var slowestIdx int
	var maxDuration int64
	for idx := range slowEvents.SlowEventList {
		event := slowEvents.SlowEventList[idx]
		if event.DurationUs > maxDuration {
			slowestIdx = idx
			maxDuration = event.DurationUs
		}
	}

	// analyze the slowest event's info
	slowestEvent := slowEvents.SlowEventList[slowestIdx]
	threadIDStr, startTime, endTime, err := analyzeSlowEvent(&slowestEvent)
	if err != nil {
		return err
	}

	sessionInfo, transactionInfo, err := opt.getEventSessionAndTxnInfo(logger, upHosts, &slowestEvent)
	if err != nil {
		return err
	}

	opt.SlowEventCascade = append(opt.SlowEventCascade, SlowEventNode{0, &slowestEvent,
		sessionInfo, transactionInfo, nil /*prior hold events*/, false})

	// recursively traceback
	const recursiveDepth = 1
	err = opt.recursiveTraceSlowEvents(logger, upHosts, threadIDStr,
		startTime, endTime, recursiveDepth)
	if err != nil {
		return err
	}

	err = opt.fillLockHoldInfo(logger, upHosts)
	if err != nil {
		return err
	}

	return err
}

func (opt *VClusterHealthOptions) recursiveTraceSlowEvents(logger vlog.Printer,
	upHosts []string,
	threadID, startTime, endTime string,
	depth int) error {
	slowEvents, err := opt.getSlowEvents(logger, upHosts, threadID, startTime, endTime, true)
	if err != nil {
		return err
	}

	// update the leaf node info
	if slowEvents == nil || len(slowEvents.SlowEventList) == 0 {
		length := len(opt.SlowEventCascade)
		opt.SlowEventCascade[length-1].Leaf = true
		return nil
	}

	for idx := range slowEvents.SlowEventList {
		event := slowEvents.SlowEventList[idx]

		sessionInfo, transactionInfo, err := opt.getEventSessionAndTxnInfo(logger, upHosts, &event)
		if err != nil {
			return err
		}

		callerThreadID, callerStartTime, callerEndTime, err := analyzeSlowEvent(&event)
		if err != nil {
			return err
		}

		// record the event
		var leaf bool
		if callerThreadID == "" {
			leaf = true
		}

		// stop recursive tracing if
		// - the caller's thread ID is empty or
		// - the caller's thread ID is same as the current event thread ID
		if callerThreadID == "" || callerThreadID == threadID {
			length := len(opt.SlowEventCascade)
			opt.SlowEventCascade[length-1].Leaf = true
			return nil
		}

		// limit the max depth
		if depth > maxDepth {
			return nil
		}

		opt.SlowEventCascade = append(opt.SlowEventCascade, SlowEventNode{depth, &event,
			sessionInfo, transactionInfo, nil, leaf})

		// go to trace the caller event
		if callerThreadID != "" && callerStartTime != "" && callerEndTime != "" {
			e := opt.recursiveTraceSlowEvents(logger, upHosts,
				callerThreadID, callerStartTime, callerEndTime,
				depth+1,
			)
			if e != nil {
				return e
			}
		}
	}

	return nil
}

func analyzeSlowEvent(event *dcSlowEvent) (
	threadIDStr, startTime, endTime string, err error) {
	phasesDurationUs := event.PhasesDurationUs
	re := regexp.MustCompile(`\[.+\]`)
	matched := re.Find([]byte(phasesDurationUs))
	matchedLengh := len(matched)
	if matchedLengh > 0 {
		threadIDHex := string(matched[1 : matchedLengh-1])
		threadIDDec := new(big.Int)
		const hex = 16
		threadIDDec.SetString(threadIDHex, hex)
		threadIDStr = threadIDDec.String()
		end, err := time.Parse(timeLayout, event.Time)
		if err != nil {
			return threadIDStr, startTime, endTime, err
		}
		// we search for the caller events that
		// - have the thread_id mentioned in phases_duration_us, and
		// - happened before (the event time) and after (the event time minus the event duration)
		start := end.Add(time.Duration(-event.DurationUs) * time.Microsecond)

		return threadIDStr, start.Format(timeLayout), end.Format(timeLayout), nil
	}

	return threadIDStr, startTime, endTime, nil
}

func (opt *VClusterHealthOptions) fillLockHoldInfo(logger vlog.Printer, upHosts []string) error {
	for i, event := range opt.SlowEventCascade {
		if !event.Leaf {
			continue
		}

		end, err := time.Parse(timeLayout, event.Event.Time)
		start := end.Add(time.Duration(-event.Event.DurationUs) * time.Microsecond)
		if err != nil {
			return nil
		}

		holdEvents, err := opt.getLockHoldSlowEvents(logger, upHosts, start.Format(timeLayout), end.Format(timeLayout))
		if err != nil {
			return err
		}
		event.PriorHoldEvents = holdEvents.SlowEventList
		opt.SlowEventCascade[i] = event
	}

	return nil
}

func (opt *VClusterHealthOptions) getEventSessionAndTxnInfo(logger vlog.Printer, upHosts []string,
	event *dcSlowEvent) (sessionInfo *dcSessionStart, transactionInfo *dcTransactionStart, err error) {
	sessionInfo, err = opt.getEventSessionInfo(logger, upHosts, event)
	if err != nil {
		return sessionInfo, transactionInfo, err
	}

	transactionInfo, err = opt.getEventTransactionInfo(logger, upHosts, event)
	if err != nil {
		return sessionInfo, transactionInfo, err
	}

	return sessionInfo, transactionInfo, err
}

func (opt *VClusterHealthOptions) getEventTransactionInfo(logger vlog.Printer, upHosts []string,
	event *dcSlowEvent) (transactionInfo *dcTransactionStart, err error) {
	transactionInfo = new(dcTransactionStart)
	if event.TxnID != "" {
		transactions, err := opt.getTransactionStarts(logger, upHosts, event.TxnID)
		if err != nil {
			return transactionInfo, err
		}
		if transactions != nil && len(transactions.TransactionStartsList) > 0 {
			transactionInfo = &transactions.TransactionStartsList[0]
		}
	}

	return transactionInfo, nil
}

func (opt *VClusterHealthOptions) getEventSessionInfo(logger vlog.Printer, upHosts []string,
	event *dcSlowEvent) (sessionInfo *dcSessionStart, err error) {
	sessionInfo = new(dcSessionStart)
	if event.SessionID != "" {
		sessions, err := opt.getSessionStarts(logger, upHosts, event.SessionID)
		if err != nil {
			return sessionInfo, err
		}
		if sessions != nil && len(sessions.SessionStartsList) > 0 {
			sessionInfo = &sessions.SessionStartsList[0]
		}
	}

	return sessionInfo, nil
}

func (opt *VClusterHealthOptions) getLockHoldSlowEvents(logger vlog.Printer, upHosts []string,
	startTime, endTime string) (slowEvents *dcSlowEvents, err error) {
	var instructions []clusterOp

	httpsSlowEventOp := makeHTTPSSlowEventOpByKeyword(upHosts, startTime, endTime,
		"hold" /*key word in phases_duration_us*/)
	instructions = append(instructions, &httpsSlowEventOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &opt.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return slowEvents, fmt.Errorf("fail to get hold-related slow events, %w", err)
	}

	return clusterOpEngine.execContext.dcSlowEvents, nil
}

func (opt *VClusterHealthOptions) DisplaySlowEventsCascade() {
	for _, eventNode := range opt.SlowEventCascade {
		indent := strings.Repeat(" ", eventNode.Depth)
		var prefix string
		if eventNode.Depth > 0 {
			prefix = "|-"
		}
		if eventNode.Leaf {
			fmt.Printf("%s%s slow_event: %+v session: %+v transaction: %+v hold_events: %d #\n",
				indent, prefix, *eventNode.Event, *eventNode.Session, *eventNode.Transaction, len(eventNode.PriorHoldEvents))
		} else {
			fmt.Printf("%s%s slow_event: %+v session: %+v transaction: %+v\n",
				indent, prefix, *eventNode.Event, *eventNode.Session, *eventNode.Transaction)
		}
	}
}
