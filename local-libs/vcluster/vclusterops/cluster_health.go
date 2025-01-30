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
	Debug             bool
	Threadhold        string
	ThreadID          string
	PhaseDurationDesc string
	EventDesc         string
	UserName          string

	// hidden option
	CascadeStack []SlowEventNode
}

type SlowEventNode struct {
	Depth int          `json:"depth"`
	Event *dcSlowEvent `json:"slow_event"`
	Leaf  bool         `json:"leaf"`
}

func VClusterHealthFactory() VClusterHealthOptions {
	options := VClusterHealthOptions{}
	// set default values to the params
	options.setDefaultValues()
	options.Debug = false

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
		_, runError = options.getSlowEvents(vcc.Log, vdb.PrimaryUpNodes, options.ThreadID, options.StartTime, options.EndTime)
	case "get_session_starts":
		runError = options.getSessionStarts(vcc.Log, vdb.PrimaryUpNodes)
	case "get_transaction_starts":
		runError = options.getTransactionStarts(vcc.Log, vdb.PrimaryUpNodes)
	default: // by default, we will build a cascade graph
		runError = options.buildCascadeGraph(vcc.Log, vdb.PrimaryUpNodes)
	}

	return runError
}

func (options *VClusterHealthOptions) buildCascadeGraph(logger vlog.Printer, upHosts []string) error {
	// get slow events during the given time
	slowEvents, err := options.getSlowEvents(logger, upHosts,
		"" /*thread_id*/, options.StartTime, options.EndTime)
	if err != nil {
		return err
	}

	// find the slowest event during the given time
	var slowestIdx int
	var maxDuration int64
	if len(slowEvents.SlowEventList) > 0 {
		for idx := range slowEvents.SlowEventList {
			event := slowEvents.SlowEventList[idx]
			if event.DurationUs > maxDuration {
				slowestIdx = idx
				maxDuration = event.DurationUs
			}
		}
	}

	// analyze the slowest event's info
	slowestEvent := slowEvents.SlowEventList[slowestIdx]
	threadIDStr, startTime, endTime, err := analyzeSlowEvent(&slowestEvent)
	if err != nil {
		return err
	}

	options.CascadeStack = append(options.CascadeStack, SlowEventNode{0, &slowestEvent, false})

	// recursively traceback
	const recursiveDepth = 1
	err = options.recursiveTraceback(logger, upHosts, threadIDStr,
		startTime, endTime, recursiveDepth)
	if err != nil {
		return err
	}

	fmt.Println("[DEBUG INFO]: cascade traceback done.")

	return err
}

func (options *VClusterHealthOptions) recursiveTraceback(logger vlog.Printer,
	upHosts []string,
	threadID, startTime, endTime string,
	depth int) error {
	slowEvents, err := options.getSlowEvents(logger, upHosts, threadID, startTime, endTime)
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

		// TODO: get txn desc and session desc here

		callerThreadID, callerStartTime, callerEndTime, err := analyzeSlowEvent(&event)
		if err != nil {
			return err
		}

		// record the event
		var leaf bool
		if callerThreadID == "" {
			leaf = true
		}
		options.CascadeStack = append(options.CascadeStack, SlowEventNode{depth, &event, leaf})

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
	const timeLayout = "2006-01-02 15:04:05.000000"

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

func (options *VClusterHealthOptions) getSlowEvents(logger vlog.Printer, upHosts []string,
	threadID, startTime, endTime string) (slowEvents *dcSlowEvents, err error) {
	var instructions []clusterOp

	httpsSlowEventOp := makeHTTPSSlowEventOp(upHosts, startTime, endTime,
		options.TxnID, options.NodeName, threadID,
		options.PhaseDurationDesc, options.EventDesc, options.Debug)
	instructions = append(instructions, &httpsSlowEventOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return slowEvents, fmt.Errorf("fail to retrieve database configurations, %w", err)
	}

	return clusterOpEngine.execContext.slowEvents, nil
}

func (options *VClusterHealthOptions) getSessionStarts(logger vlog.Printer, upHosts []string) error {
	var instructions []clusterOp

	httpsSessionStartsOp := makeHTTPSSessionStartsOp(upHosts, options.SessionID, options.StartTime, options.EndTime, false)
	instructions = append(instructions, &httpsSessionStartsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err := clusterOpEngine.run(logger)
	if err != nil {
		return fmt.Errorf("fail to retrieve database configurations, %w", err)
	}

	fmt.Printf("[DEBUG INFO] %+v\n", clusterOpEngine.execContext.dcSessionStarts)

	return nil
}

func (options *VClusterHealthOptions) getTransactionStarts(logger vlog.Printer, upHosts []string) error {
	var instructions []clusterOp

	httpsTransactionStartsOp := makeHTTPSTransactionStartsOp(upHosts, options.TxnID, options.StartTime, options.EndTime, false)
	instructions = append(instructions, &httpsTransactionStartsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err := clusterOpEngine.run(logger)
	if err != nil {
		return fmt.Errorf("fail to retrieve database configurations, %w", err)
	}

	fmt.Printf("[DEBUG INFO] %+v\n", clusterOpEngine.execContext.dcTransactionStarts)

	return nil
}
