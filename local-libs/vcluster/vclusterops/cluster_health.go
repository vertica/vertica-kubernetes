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

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// VClusterHealthOptions represents the available options to check the cluster health
type VClusterHealthOptions struct {
	DatabaseOptions
	Operation         string
	TxnID             string
	NodeName          string
	StartTime         string
	EndTime           string
	SessionID         string
	Threadhold        string
	ThreadID          string
	PhaseDurationDesc string
	EventDesc         string
	UserName          string
	Display           bool
	Timezone          string

	// hidden option
	CascadeStack            []SlowEventNode
	SessionStartsResult     *dcSessionStarts
	TransactionStartsResult *dcTransactionStarts
	SlowEventsResult        *dcSlowEvents
}

type SlowEventNode struct {
	Depth           int                 `json:"depth"`
	Event           *dcSlowEvent        `json:"slow_event"`
	Session         *dcSessionStart     `json:"session"`
	Transaction     *dcTransactionStart `json:"transaction"`
	PriorHoldEvents []dcSlowEvent       `json:"prior_hold_events"`
	Leaf            bool                `json:"leaf"`
}

const timeLayout = "2006-01-02 15:04:05.999999"
const maxDepth = 100

func VClusterHealthFactory() VClusterHealthOptions {
	options := VClusterHealthOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VClusterHealthOptions) setDefaultValues() {
	options.DatabaseOptions.setDefaultValues()
}

func (options *VClusterHealthOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := options.validateBaseOptions(ClusterHealthCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VClusterHealthOptions) validateExtraOptions() error {
	// data prefix
	if options.DataPrefix != "" {
		return util.ValidateRequiredAbsPath(options.DataPrefix, "data path")
	}
	return nil
}

func (options *VClusterHealthOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required params
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}
	// batch 2: validate all other params
	err = options.validateExtraOptions()
	if err != nil {
		return err
	}
	return nil
}

func (options *VClusterHealthOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.Hosts, err = util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.normalizePaths()
	}

	// analyze start and end time
	if options.Timezone != "" {
		err := options.convertDateStringToUTC()
		if err != nil {
			return err
		}
	}

	return nil
}

func (options *VClusterHealthOptions) convertDateStringToUTC() error {
	// convert start time to UTC
	if options.StartTime != "" {
		startTime, err := util.ConvertDateStringToUTC(options.StartTime, options.Timezone)
		if err != nil {
			return err
		}
		options.StartTime = startTime
	}

	// convert end time to UTC
	if options.EndTime != "" {
		endTime, err := util.ConvertDateStringToUTC(options.EndTime, options.Timezone)
		if err != nil {
			return err
		}
		options.EndTime = endTime
	}

	return nil
}

func (options *VClusterHealthOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := options.validateParseOptions(log); err != nil {
		return err
	}
	err := options.analyzeOptions()
	if err != nil {
		return err
	}
	return options.setUsePasswordAndValidateUsernameIfNeeded(log)
}

func (vcc VClusterCommands) VClusterHealth(options *VClusterHealthOptions) error {
	vdb := makeVCoordinationDatabase()

	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	err = vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return err
	}

	operation := options.Operation
	var runError error
	switch operation {
	case "get_slow_events":
		options.SlowEventsResult, runError = options.getSlowEvents(vcc.Log, vdb.PrimaryUpNodes, options.ThreadID, options.StartTime,
			options.EndTime, false /*Not for cascade*/)
	case "get_session_starts":
		options.SessionStartsResult, runError = options.getSessionStarts(vcc.Log, vdb.PrimaryUpNodes, options.SessionID)
	case "get_transaction_starts":
		options.TransactionStartsResult, runError = options.getTransactionStarts(vcc.Log, vdb.PrimaryUpNodes, options.TxnID)
	default: // by default, we will build a cascade graph
		runError = options.buildCascadeGraph(vcc.Log, vdb.PrimaryUpNodes)
	}

	return runError
}

func (options *VClusterHealthOptions) buildCascadeGraph(logger vlog.Printer, upHosts []string) error {
	// get slow events during the given time
	slowEvents, err := options.getSlowEvents(logger, upHosts,
		"" /*thread_id*/, options.StartTime, options.EndTime, true /*for cascade*/)
	if err != nil {
		return err
	}

	// find the slowest event during the given time
	if len(slowEvents.SlowEventList) == 0 {
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

	sessionInfo, transactionInfo, err := options.getEventSessionAndTxnInfo(logger, upHosts, &slowestEvent)
	if err != nil {
		return err
	}

	options.CascadeStack = append(options.CascadeStack, SlowEventNode{0, &slowestEvent,
		sessionInfo, transactionInfo, nil /*prior hold events*/, false})

	// recursively traceback
	const recursiveDepth = 1
	err = options.recursiveTraceback(logger, upHosts, threadIDStr,
		startTime, endTime, recursiveDepth)
	if err != nil {
		return err
	}

	err = options.fillLockHoldInfo(logger, upHosts)
	if err != nil {
		return err
	}

	return err
}

func (options *VClusterHealthOptions) recursiveTraceback(logger vlog.Printer,
	upHosts []string,
	threadID, startTime, endTime string,
	depth int) error {
	slowEvents, err := options.getSlowEvents(logger, upHosts, threadID, startTime, endTime, true)
	if err != nil {
		return err
	}

	// update the leaf node info
	if len(slowEvents.SlowEventList) == 0 {
		length := len(options.CascadeStack)
		options.CascadeStack[length-1].Leaf = true
		return nil
	}

	for idx := range slowEvents.SlowEventList {
		event := slowEvents.SlowEventList[idx]

		sessionInfo, transactionInfo, err := options.getEventSessionAndTxnInfo(logger, upHosts, &event)
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
			length := len(options.CascadeStack)
			options.CascadeStack[length-1].Leaf = true
			return nil
		}

		// limit the max depth
		if depth > maxDepth {
			return nil
		}

		options.CascadeStack = append(options.CascadeStack, SlowEventNode{depth, &event,
			sessionInfo, transactionInfo, nil, leaf})

		// go to trace the caller event
		if callerThreadID != "" && callerStartTime != "" && callerEndTime != "" {
			e := options.recursiveTraceback(logger, upHosts,
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

func (options *VClusterHealthOptions) fillLockHoldInfo(logger vlog.Printer, upHosts []string) error {
	for i, event := range options.CascadeStack {
		if !event.Leaf {
			continue
		}

		end, err := time.Parse(timeLayout, event.Event.Time)
		start := end.Add(time.Duration(-event.Event.DurationUs) * time.Microsecond)
		if err != nil {
			return nil
		}

		holdEvents, err := options.getLockHoldEvents(logger, upHosts, start.Format(timeLayout), end.Format(timeLayout))
		if err != nil {
			return err
		}
		event.PriorHoldEvents = holdEvents.SlowEventList
		options.CascadeStack[i] = event
	}

	return nil
}

func (options *VClusterHealthOptions) getSlowEvents(logger vlog.Printer, upHosts []string,
	threadID, startTime, endTime string, forCascade bool) (slowEvents *dcSlowEvents, err error) {
	var instructions []clusterOp

	if forCascade {
		httpsSlowEventWithThreadIDOp := makeHTTPSSlowEventOpByThreadID(upHosts, startTime, endTime,
			threadID)
		instructions = append(instructions, &httpsSlowEventWithThreadIDOp)
	} else {
		httpsSlowEventOp := makeHTTPSSlowEventOp(upHosts, startTime, endTime,
			threadID, options.PhaseDurationDesc, options.TxnID, options.EventDesc, options.NodeName)
		instructions = append(instructions, &httpsSlowEventOp)
	}

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return slowEvents, fmt.Errorf("fail to retrieve database configurations, %w", err)
	}
	return clusterOpEngine.execContext.slowEvents, nil
}

func (options *VClusterHealthOptions) getLockHoldEvents(logger vlog.Printer, upHosts []string,
	startTime, endTime string) (slowEvents *dcSlowEvents, err error) {
	var instructions []clusterOp

	httpsSlowEventOp := makeHTTPSSlowEventOpByKeyword(upHosts, startTime, endTime,
		"hold" /*key word in phases_duration_us*/)
	instructions = append(instructions, &httpsSlowEventOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return slowEvents, fmt.Errorf("fail to retrieve database configurations, %w", err)
	}

	return clusterOpEngine.execContext.slowEvents, nil
}

func (options *VClusterHealthOptions) getSessionStarts(logger vlog.Printer, upHosts []string,
	sessionID string) (sessionStarts *dcSessionStarts, err error) {
	var instructions []clusterOp

	httpsSessionStartsOp := makeHTTPSSessionStartsOp(upHosts, sessionID,
		options.StartTime, options.EndTime)
	instructions = append(instructions, &httpsSessionStartsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return sessionStarts, fmt.Errorf("fail to retrieve database configurations, %w", err)
	}

	return clusterOpEngine.execContext.dcSessionStarts, nil
}

func (options *VClusterHealthOptions) getEventSessionInfo(logger vlog.Printer, upHosts []string,
	event *dcSlowEvent) (sessionInfo *dcSessionStart, err error) {
	sessionInfo = new(dcSessionStart)
	if event.SessionID != "" {
		sessions, err := options.getSessionStarts(logger, upHosts, event.SessionID)
		if err != nil {
			return sessionInfo, err
		}
		if len(sessions.SessionStartsList) > 0 {
			sessionInfo = &sessions.SessionStartsList[0]
		}
	}

	return sessionInfo, nil
}

func (options *VClusterHealthOptions) getTransactionStarts(logger vlog.Printer, upHosts []string,
	txnID string) (transactionInfo *dcTransactionStarts, err error) {
	var instructions []clusterOp

	httpsTransactionStartsOp := makeHTTPSTransactionStartsOp(upHosts, txnID,
		options.StartTime, options.EndTime)
	instructions = append(instructions, &httpsTransactionStartsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return transactionInfo, fmt.Errorf("fail to retrieve database configurations, %w", err)
	}

	return clusterOpEngine.execContext.dcTransactionStarts, nil
}

func (options *VClusterHealthOptions) getEventTransactionInfo(logger vlog.Printer, upHosts []string,
	event *dcSlowEvent) (transactionInfo *dcTransactionStart, err error) {
	transactionInfo = new(dcTransactionStart)
	if event.TxnID != "" {
		transactions, err := options.getTransactionStarts(logger, upHosts, event.TxnID)
		if err != nil {
			return transactionInfo, err
		}
		if len(transactions.TransactionStartsList) > 0 {
			transactionInfo = &transactions.TransactionStartsList[0]
		}
	}

	return transactionInfo, nil
}

func (options *VClusterHealthOptions) getEventSessionAndTxnInfo(logger vlog.Printer, upHosts []string,
	event *dcSlowEvent) (sessionInfo *dcSessionStart, transactionInfo *dcTransactionStart, err error) {
	sessionInfo, err = options.getEventSessionInfo(logger, upHosts, event)
	if err != nil {
		return sessionInfo, transactionInfo, err
	}

	transactionInfo, err = options.getEventTransactionInfo(logger, upHosts, event)
	if err != nil {
		return sessionInfo, transactionInfo, err
	}

	return sessionInfo, transactionInfo, err
}

func (options *VClusterHealthOptions) DisplayCascadeGraph() {
	for _, eventNode := range options.CascadeStack {
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
