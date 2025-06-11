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
	"strconv"
	"strings"
	"time"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type SlowEventNode struct {
	Depth int          `json:"depth"`
	Event *dcSlowEvent `json:"slow_event"`
	// Session         *dcSessionStart     `json:"session"`
	// Transaction     *dcTransactionStart `json:"transaction"`
	PriorHoldEvents *[]dcSlowEvent   `json:"prior_hold_events"`
	Children        []*SlowEventNode `json:"children"`
	Leaf            bool             `json:"leaf"`
}

const (
	lookbackTime      = 5 * time.Minute
	maxNodes          = 100
	internalSessionID = "NO NODE:0x1"
	maxDepth          = 10
	maxHoldEvents     = 5
)

type eventMapEntry struct {
	threadID string

	event     *dcSlowEvent
	processed bool
}

func (opt *VClusterHealthOptions) buildCascadeGraph(logger vlog.Printer, upHosts []string) error {
	originalStart, originalEnd, err := parseTimeRange(opt.StartTime, opt.EndTime)
	if err != nil {
		return err
	}

	allSlowEvents, err := opt.loadAllMutexEvents(logger, upHosts, originalStart)
	if err != nil {
		return err
	}
	if allSlowEvents == nil || len(*allSlowEvents) == 0 {
		logger.PrintInfo("No slow events found in the time range")
		return nil
	}

	logger.PrintInfo("Building cascade graph for mutex events")

	slowestEvent, err := findSlowestEventInTimeRange(logger, allSlowEvents, originalStart, originalEnd)
	if err != nil {
		return fmt.Errorf("failed to find slowest event: %w", err)
	}
	if slowestEvent == nil {
		logger.PrintInfo("No slow events found in the specified time range")
		return nil
	}

	eventMap, err := buildEventMap(allSlowEvents)
	if err != nil {
		return fmt.Errorf("failed to build event map: %w", err)
	}

	sessionIDs := make(map[string]any)
	txnIDs := make(map[string]any)
	const initialDepth = 0
	rootNode, err := buildCascadeTree(slowestEvent, eventMap, initialDepth, maxDepth, sessionIDs, txnIDs)
	if err != nil {
		return fmt.Errorf("failed to build cascade: %w", err)
	}

	opt.SlowEventCascade = flattenTree(rootNode)

	if err := opt.fillLockHoldInfo(logger, allSlowEvents, sessionIDs, txnIDs); err != nil {
		return fmt.Errorf("failed to fill lock hold info: %w", err)
	}
	if opt.NeedSessionTnxInfo {
		if err := opt.attachSessionTxnInfo(sessionIDs, txnIDs, logger, upHosts); err != nil {
			return err
		}
	}
	return nil
}

// loadAllSlowEvents loads all slow events in the extended time range.
func (opt *VClusterHealthOptions) loadAllMutexEvents(logger vlog.Printer,
	upHosts []string, originalStart time.Time) (*[]dcSlowEvent, error) {
	extendedStart := originalStart.Add(-lookbackTime).Format(timeLayout)
	allSlowEvents, err := opt.getSlowEvents(logger, upHosts, "", extendedStart, opt.EndTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get mutex events: %w", err)
	}
	return allSlowEvents, nil
}

// attachSessionTxnInfo attaches session and transaction info to the slow event cascade.
func (opt *VClusterHealthOptions) attachSessionTxnInfo(sessionIDs, txnIDs map[string]any, logger vlog.Printer, upHosts []string) error {
	sessions, txns, err := opt.getSessionTxnInfo(sessionIDs, txnIDs, logger, upHosts)
	if err != nil {
		return fmt.Errorf("failed to get session/txn info: %w", err)
	}
	for i, node := range opt.SlowEventCascade {
		opt.attachSessionTxnToNode(i, node, sessions, txns)
	}
	return nil
}

// attachSessionTxnToNode attaches session and txn info to a single node and its hold events.
func (opt *VClusterHealthOptions) attachSessionTxnToNode(i int, node SlowEventNode,
	sessions map[string]*dcSessionStarts, txns map[string]*dcTransactionStarts) {
	if node.Event != nil {
		opt.attachSessionTxnToEvent(i, node.Event, sessions, txns)
		if node.PriorHoldEvents != nil {
			for j := range *node.PriorHoldEvents {
				holdEvent := &(*node.PriorHoldEvents)[j]
				opt.attachSessionTxnToEvent(i, holdEvent, sessions, txns)
			}
		}
	}
}

// attachSessionTxnToEvent attaches session and txn info to a single event.
func (opt *VClusterHealthOptions) attachSessionTxnToEvent(i int, event *dcSlowEvent,
	sessions map[string]*dcSessionStarts, txns map[string]*dcTransactionStarts) {
	if sessions != nil && event.getSessionID() != "" {
		if sess, ok := sessions[event.getSessionID()]; ok {
			opt.SlowEventCascade[i].Event.SessionInfo = sess
		}
	}
	if txns != nil && event.getTxnID() != "" {
		if txn, ok := txns[event.getTxnID()]; ok {
			opt.SlowEventCascade[i].Event.TxnInfo = txn
		}
	}
}

func findSlowestEventInTimeRange(logger vlog.Printer, events *[]dcSlowEvent, startTime, endTime time.Time) (*dcSlowEvent, error) {
	var slowestEvent *dcSlowEvent
	var maxDuration int64

	for i := range *events {
		event := &(*events)[i]
		if event.getSessionID() == internalSessionID {
			// skip all internal events
			continue
		}
		eventTime, err := time.Parse(timeLayout, event.Time)
		if err != nil {
			return nil, err
		}

		// Check if event is within the original time range
		if eventTime.Before(startTime) || eventTime.After(endTime) {
			continue
		}

		durationInt, err := strconv.ParseInt(event.DurationUs, 10, 64)
		if err != nil {
			return nil, err
		}
		if slowestEvent == nil || durationInt > maxDuration {
			slowestEvent = event
			maxDuration = durationInt
		}
	}
	if slowestEvent != nil {
		logger.PrintInfo("Slowest event found: %s node: %s with duration %d us", slowestEvent.Time, slowestEvent.NodeName, maxDuration)
	}
	return slowestEvent, nil
}

func buildEventMap(events *[]dcSlowEvent) (map[string][]*eventMapEntry, error) {
	eventMap := make(map[string][]*eventMapEntry)
	for i := range *events {
		event := &(*events)[i]
		threadID := event.getThreadID()
		if threadID != "" {
			eventMap[threadID] = append(eventMap[threadID], &eventMapEntry{
				threadID:  threadID,
				event:     event,
				processed: false,
			})
		}
	}
	return eventMap, nil
}

func buildCascadeTree(parentEvent *dcSlowEvent, eventMap map[string][]*eventMapEntry, currentDepth,
	maxDepth int, sessionIDs, txnIDs map[string]any) (*SlowEventNode, error) {
	if currentDepth > maxDepth {
		return nil, nil
	}

	updateSessionAndTxnIDs(parentEvent, sessionIDs, txnIDs)

	parentNode := &SlowEventNode{
		Depth: currentDepth,
		Event: parentEvent,
		Leaf:  true,
	}

	threadIDs := analyzeSlowEvent(parentEvent)
	currentTime, err := time.Parse(timeLayout, parentEvent.Time)
	if err != nil {
		return nil, err
	}

	children, err := findChildNodes(threadIDs, eventMap, currentTime, currentDepth, maxDepth, sessionIDs, txnIDs)
	if err != nil {
		return nil, err
	}
	if len(children) > 0 {
		parentNode.Children = children
		parentNode.Leaf = false
	}

	return parentNode, nil
}

// updateSessionAndTxnIDs adds session and txn IDs to their respective maps if present.
func updateSessionAndTxnIDs(event *dcSlowEvent, sessionIDs, txnIDs map[string]any) {
	if event.getSessionID() != "" && event.getSessionID() != internalSessionID {
		sessionIDs[event.getSessionID()] = struct{}{}
	}
	if event.getTxnID() != "" {
		txnIDs[event.getTxnID()] = struct{}{}
	}
}

// findChildNodes processes threadIDs and returns child nodes for the cascade tree.
func findChildNodes(threadIDs []string, eventMap map[string][]*eventMapEntry, currentTime time.Time,
	currentDepth, maxDepth int, sessionIDs, txnIDs map[string]any) ([]*SlowEventNode, error) {
	var children []*SlowEventNode
	for _, threadID := range threadIDs {
		candidateEvents, exists := eventMap[threadID]
		if !exists {
			continue
		}
		childNodes, err := processCandidateEvents(candidateEvents, currentTime, currentDepth, maxDepth, eventMap, sessionIDs, txnIDs)
		if err != nil {
			return nil, err
		}
		children = append(children, childNodes...)
	}
	return children, nil
}

// processCandidateEvents processes candidate events for a threadID and returns child nodes.
func processCandidateEvents(candidateEvents []*eventMapEntry, currentTime time.Time, currentDepth, maxDepth int,
	eventMap map[string][]*eventMapEntry, sessionIDs, txnIDs map[string]any) ([]*SlowEventNode, error) {
	var children []*SlowEventNode
	for _, entry := range candidateEvents {
		if entry.processed {
			continue
		}
		candidateTime, err := time.Parse(timeLayout, entry.event.Time)
		if err != nil {
			return nil, err
		}
		if candidateTime.Before(currentTime) {
			entry.processed = true
			childNode, err := buildCascadeTree(entry.event, eventMap, currentDepth+1, maxDepth, sessionIDs, txnIDs)
			if err != nil {
				return nil, err
			}
			if childNode != nil {
				children = append(children, childNode)
			}
		}
	}
	return children, nil
}

func flattenTree(root *SlowEventNode) []SlowEventNode {
	var result []SlowEventNode
	if root == nil {
		return result
	}

	var dfs func(node *SlowEventNode)
	dfs = func(node *SlowEventNode) {
		// Add current node to result (without children for flat structure)
		flatNode := SlowEventNode{
			Depth:           node.Depth,
			Event:           node.Event,
			PriorHoldEvents: node.PriorHoldEvents,
			Leaf:            node.Leaf,
		}
		result = append(result, flatNode)

		// Recursive process children
		for _, child := range node.Children {
			dfs(child)
		}
	}

	dfs(root)
	return result
}

// find out all thread IDs in the phasesDurationUs field
// convert to Dec and return them as a slice of strings
func analyzeSlowEvent(event *dcSlowEvent) (
	threadIDStrs []string) {
	phasesDurationUs := event.PhasesDurationUs
	if phasesDurationUs == "" {
		return threadIDStrs
	}
	re := regexp.MustCompile(`\[.+?\]`)
	matched := re.FindAll([]byte(phasesDurationUs), -1)

	for _, match := range matched {
		threadIDHex := string(match[1 : len(match)-1])
		threadIDDec := new(big.Int)
		const hex = 16
		threadIDDec.SetString(threadIDHex, hex)
		// Append threadIDDec only if it's not already in threadIDStrs
		exists := false
		for _, id := range threadIDStrs {
			if id == threadIDDec.String() {
				exists = true
				break
			}
		}
		if !exists {
			threadIDStrs = append(threadIDStrs, threadIDDec.String())
		}
	}
	return threadIDStrs
}

func (opt *VClusterHealthOptions) fillLockHoldInfo(logger vlog.Printer, events *[]dcSlowEvent, sessionIDs, txnIDs map[string]any) error {
	for i, event := range opt.SlowEventCascade {
		if !event.Leaf {
			continue
		}

		end, err := time.Parse(timeLayout, event.Event.Time)
		if err != nil {
			return nil
		}

		durationInt, err := strconv.ParseInt(event.Event.DurationUs, 10, 64)
		if err != nil {
			return err
		}
		start := end.Add(time.Duration(-durationInt) * time.Microsecond)

		holdEvents, err := opt.getLockHoldSlowEvents(logger, events, start.Format(timeLayout), end.Format(timeLayout))
		if err != nil {
			return err
		}
		for j := range *holdEvents {
			holdEvent := &(*holdEvents)[j]
			if holdEvent.getSessionID() != "" && holdEvent.getSessionID() != internalSessionID {
				sessionIDs[holdEvent.getSessionID()] = struct{}{}
			}
			if holdEvent.getTxnID() != "" {
				txnIDs[holdEvent.getTxnID()] = struct{}{}
			}
		}
		event.PriorHoldEvents = holdEvents
		opt.SlowEventCascade[i] = event
	}

	return nil
}

func (opt *VClusterHealthOptions) getLockHoldSlowEvents(logger vlog.Printer, events *[]dcSlowEvent,
	startTime, endTime string) (slowEvents *[]dcSlowEvent, err error) {
	var holdEvents []dcSlowEvent
	// Check if the event falls within the specified time range
	start, err := time.Parse(timeLayout, startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to parse start time: %w", err)
	}
	end, err := time.Parse(timeLayout, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to parse end time: %w", err)
	}

	lockHoldCount := 0
	// Iterate through already loaded slow events
	for i := range *events {
		event := &(*events)[i]
		eventTime, err := time.Parse(timeLayout, event.Time)
		if err != nil {
			return nil, fmt.Errorf("failed to parse event time: %w", err)
		}

		if eventTime.After(start) && eventTime.Before(end) {
			// Check if the "phasesDurationUs" field contains "hold"
			if strings.Contains(event.PhasesDurationUs, "hold") {
				holdEvents = append(holdEvents, *event)
				lockHoldCount++
				if lockHoldCount > maxHoldEvents {
					logger.PrintInfo("Found more than %d hold events, stop searching", maxHoldEvents)
					break
				}
			}
		}
	}
	logger.PrintInfo("Found %d hold events in the specified time range", len(holdEvents))
	return &holdEvents, nil
}

// DisplaySlowEventsCascade prints the slow event cascade in a tree-like structure
func (opt *VClusterHealthOptions) DisplayMutexEventsCascade() {
	for _, eventNode := range opt.SlowEventCascade {
		indent := strings.Repeat(" ", eventNode.Depth)
		var prefix string
		if eventNode.Depth > 0 {
			prefix = "|-"
		}
		if eventNode.Leaf {
			fmt.Printf("%s%s slow_event: %+v hold_events: %d #\n",
				indent, prefix, *eventNode.Event, len(*eventNode.PriorHoldEvents))
		} else {
			fmt.Printf("%s%s slow_event: %+v \n",
				indent, prefix, *eventNode.Event)
		}
	}
}

func (opt *VClusterHealthOptions) getSessionTxnInfo(sessionIDs, txnIDs map[string]any, logger vlog.Printer,
	upHosts []string) (sessionMap map[string]*dcSessionStarts, txnMap map[string]*dcTransactionStarts, err error) {
	// If we don't need session or transaction info, return early
	sessionMap = make(map[string]*dcSessionStarts)
	txnMap = make(map[string]*dcTransactionStarts)
	if !opt.NeedSessionTnxInfo {
		logger.PrintInfo("Skipping session and transaction info retrieval")
		return sessionMap, txnMap, nil
	}
	sessionStr := util.JoinMapKeys(sessionIDs, ",")
	txnStr := util.JoinMapKeys(txnIDs, ",")
	if sessionStr == "" && txnStr == "" {
		logger.PrintInfo("No session or transaction IDs found, skipping retrieval")
		return sessionMap, txnMap, nil
	}
	opt.StartTime = ""
	opt.EndTime = "" // reset time range to avoid unnecessary filtering for sessions and transactions
	sessions, err := opt.getSessionStarts(logger, upHosts, sessionStr)
	if err != nil {
		return sessionMap, txnMap, fmt.Errorf("failed to get session starts: %w", err)
	}
	txns, err := opt.getTransactionStarts(logger, upHosts, txnStr)
	if err != nil {
		return sessionMap, txnMap, fmt.Errorf("failed to get transaction starts: %w", err)
	}
	// convert sessions and txns to maps for easy lookup
	for i := range *sessions {
		sess := &(*sessions)[i]
		sessionMap[sess.SessionID] = sess
	}
	for i := range *txns {
		txn := &(*txns)[i]
		txnMap[txn.TxnID] = txn
	}
	return sessionMap, txnMap, nil
}
