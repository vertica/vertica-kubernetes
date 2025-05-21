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
	// Parse original time range
	originalStart, err := time.Parse(timeLayout, opt.StartTime)
	if err != nil {
		return fmt.Errorf("failed to parse start time: %w", err)
	}
	originalEnd, err := time.Parse(timeLayout, opt.EndTime)
	if err != nil {
		return fmt.Errorf("failed to parse end time: %w", err)
	}

	// Calculate extended time range (startTime - 45min to endTime)
	extendedStart := originalStart.Add(-lookbackTime).Format(timeLayout)

	// Get all slow events in the extended time range
	allSlowEvents, err := opt.getSlowEvents(logger, upHosts, "" /*thread_id*/, extendedStart, opt.EndTime)
	if err != nil {
		return fmt.Errorf("failed to get slow events: %w", err)
	}
	if allSlowEvents == nil || len(*allSlowEvents) == 0 {
		logger.PrintInfo("No slow events found in the time range")
		return nil
	}
	fmt.Println("Found slow events:", len(*allSlowEvents))

	logger.PrintInfo("Building cascade graph for slow events")

	// Find the slowest event within the original time range
	slowestEvent, err := findSlowestEventInTimeRange(allSlowEvents, originalStart, originalEnd)
	if err != nil {
		return fmt.Errorf("failed to find slowest event: %w", err)
	}
	if slowestEvent == nil {
		logger.PrintInfo("No slow events found in the specified time range")
		return nil
	}

	// Build event map for quick lookup
	eventMap, err := buildEventMap(allSlowEvents)
	if err != nil {
		return fmt.Errorf("failed to build event map: %w", err)
	}
	// Build the cascade graph iteratively
	rootNode, err := buildCascadeTree(slowestEvent, eventMap, 0, maxDepth)
	if err != nil {
		return fmt.Errorf("failed to build cascade: %w", err)
	}

	// Convert tree to flat list for backward compatibility
	opt.SlowEventCascade = flattenTree(rootNode)

	// Fill lock hold information
	err = opt.fillLockHoldInfo(logger, allSlowEvents)
	if err != nil {
		return fmt.Errorf("failed to fill lock hold info: %w", err)
	}

	return nil
}

func findSlowestEventInTimeRange(events *[]dcSlowEvent, startTime, endTime time.Time) (*dcSlowEvent, error) {
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

func buildCascadeTree(parentEvent *dcSlowEvent, eventMap map[string][]*eventMapEntry, currentDepth, maxDepth int) (*SlowEventNode, error) {
	if currentDepth > maxDepth {
		return nil, nil
	}

	// Create root node
	parentNode := &SlowEventNode{
		Depth: currentDepth,
		Event: parentEvent,
		Leaf:  true, // Assume leaf until we find children
	}

	// Extract thread ID from the root event
	threadIDs := analyzeSlowEvent(parentEvent)
	currentTime, err := time.Parse(timeLayout, parentEvent.Time)
	if err != nil {
		return nil, err
	}

	// Find all events that this event is waiting for (child events)
	for _, threadID := range threadIDs {
		if candidateEvents, exists := eventMap[threadID]; exists {
			for _, entry := range candidateEvents {
				if entry.processed {
					continue
				}

				// Check if the candidate event happened before our current event
				candidateTime, err := time.Parse(timeLayout, entry.event.Time)
				if err != nil {
					return nil, err
				}
				if candidateTime.Before(currentTime) {
					// Mark as processed to avoid cycles
					entry.processed = true

					// Recursively build the child tree
					childNode, err := buildCascadeTree(entry.event, eventMap, currentDepth+1, maxDepth)
					if err != nil {
						return nil, err
					}

					if childNode != nil {
						parentNode.Children = append(parentNode.Children, childNode)
						parentNode.Leaf = false // We found at least one child
					}
				}
			}
		}
	}

	return parentNode, nil
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

		// Recursively process children
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

func (opt *VClusterHealthOptions) fillLockHoldInfo(logger vlog.Printer, events *[]dcSlowEvent) error {
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
					logger.PrintInfo("Found more than %d hold events, stopping search", maxHoldEvents)
					break
				}
			}
		}
	}
	logger.PrintInfo("Found %d hold events in the specified time range", len(holdEvents))
	return &holdEvents, nil
}

func (opt *VClusterHealthOptions) DisplaySlowEventsCascade() {
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
