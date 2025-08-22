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
	"sort"
	"strconv"
	"strings"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type NodeLockEvents struct {
	NodeName        string
	MaxDuration     string            `json:"max_duration"`
	WaitStartTime   string            `json:"wait_start_time"`
	WaitEndTime     string            `json:"wait_end_time"`
	TotalWaitEvents int               `json:"total_wait_events"`
	LockWaitEvents  *[]dcLockAttempts `json:"wait_locks"`
	LockHoldEvents  *[]dcLockReleases `json:"hold_locks"` // hold locks related to earliest wait locks
}

type parsedEvent struct {
	Original        any
	NodeName        string
	Duration        time.Duration
	TotalWaitEvents int
	Start           time.Time
	End             time.Time
	Processed       bool
}

const (
	maxLockWaitEvents = 10    // max number of wait events to analyze in one series
	lockAttemptsLimit = 40960 // max number of lock attempts to load from database in one batch
	lockReleasesLimit = 10240 // max number of lock releases to load from database in one batch
	// as lock will timeout in 45 minutes, we need to check the lock attempts in the previous 45 minutes
	lockEventsTraceBack     = -45 * time.Minute
	lockWaitSeriesInSeconds = 10 // min duration of lock wait series in seconds to be considered for analysis
)

// parseCustomDuration parses a custom duration string in the format HH:MM:SS[.fff...]
func parseCustomDuration(timeStr string) (time.Duration, error) {
	const (
		secondsSplitParts = 2
		nanosecondDigits  = 9
	)

	parts := strings.Split(timeStr, ":")
	if len(parts) < 3 || len(parts) > 3 {
		return 0, fmt.Errorf("invalid time format, expected HH:MM:SS[.fff...]")
	}

	// Parse hours
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid hours: %v", err)
	}

	// Parse minutes
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid minutes: %v", err)
	}

	// Parse seconds and optional fractional seconds
	secondsParts := strings.SplitN(parts[2], ".", secondsSplitParts)
	seconds, err := strconv.Atoi(secondsParts[0])
	if err != nil {
		return 0, fmt.Errorf("invalid seconds: %v", err)
	}

	// Handle fractional seconds if they exist
	nanoseconds := 0
	if len(secondsParts) == secondsSplitParts {
		fracStr := secondsParts[1]
		// Pad or truncate to 9 digits (nanoseconds)
		if len(fracStr) > nanosecondDigits {
			fracStr = fracStr[:nanosecondDigits]
		} else {
			fracStr += strings.Repeat("0", nanosecondDigits-len(fracStr))
		}
		nanoseconds, err = strconv.Atoi(fracStr)
		if err != nil {
			return 0, fmt.Errorf("invalid fractional seconds: %v", err)
		}
	}

	total := time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second +
		time.Duration(nanoseconds)*time.Nanosecond

	return total, nil
}

// getLockAttempts gets the lock attempts events from the database.
func (opt *VClusterHealthOptions) getLockAttempts(logger vlog.Printer, upHosts []string,
	startTime, endTime string) (lockAttempts *[]dcLockAttempts, err error) {
	var instructions []clusterOp

	nmaLockAttemptsOp, err := makeNMALockAttemptsOp(upHosts, opt.DatabaseOptions.UserName,
		opt.DatabaseOptions.DBName, opt.DatabaseOptions.Password,
		startTime, endTime, "" /*node name*/, opt.LockAttemptThresHold, lockAttemptsLimit)
	if err != nil {
		return nil, err
	}
	instructions = append(instructions, &nmaLockAttemptsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &opt.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return nil, fmt.Errorf("fail to get lock waiting events, %w", err)
	}
	return clusterOpEngine.execContext.dcLockAttemptsList, nil
}

// getLockReleases gets the lock releases events from the database.
func (opt *VClusterHealthOptions) getLockReleases(logger vlog.Printer,
	upHosts []string, startTime, endTime string) (lockReleases *[]dcLockReleases, err error) {
	var instructions []clusterOp
	nmaLockAttemptsOp, err := makeNMALockReleasesOp(upHosts, opt.DatabaseOptions.UserName,
		opt.DatabaseOptions.DBName, opt.DatabaseOptions.Password,
		startTime, endTime, "", lockReleasesLimit, opt.LockReleaseThresHold)
	if err != nil {
		return nil, err
	}
	instructions = append(instructions, &nmaLockAttemptsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &opt.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return nil, fmt.Errorf("fail to get lock holding events, %w", err)
	}
	return clusterOpEngine.execContext.dcLockReleasesList, nil
}

// parseAttemptsEvents parses the lock attempts events and groups them by node name.
func parseAttemptsEvents(events *[]dcLockAttempts) (map[string][]parsedEvent, error) {
	if events == nil || len(*events) == 0 {
		return nil, nil
	}

	var parsed []parsedEvent

	for i := range *events {
		e := &(*events)[i]
		start, err := time.Parse(timeLayout, e.StartTime)
		if err != nil {
			return nil, fmt.Errorf("invalid StartTime: %v", err)
		}
		dur, err := parseCustomDuration(e.Duration)
		if err != nil {
			return nil, fmt.Errorf("invalid Duration: %v", err)
		}
		parsed = append(parsed, parsedEvent{
			Original: *e,
			NodeName: e.NodeName,
			Duration: dur,
			Start:    start,
			End:      start.Add(dur),
		})
	}
	// group parsed events by node_name
	grouped := make(map[string][]parsedEvent)
	for _, event := range parsed {
		grouped[event.NodeName] = append(grouped[event.NodeName], event)
	}

	return grouped, nil
}

// parseReleasesEvents parses the lock releases events and groups them by node name.
func parseReleasesEvents(events *[]dcLockReleases) (map[string][]parsedEvent, error) {
	if events == nil || len(*events) == 0 {
		return nil, nil
	}
	var parsed []parsedEvent
	for i := range *events {
		e := &(*events)[i]
		start, err := time.Parse(timeLayout, e.GrantTime)
		if err != nil {
			return nil, fmt.Errorf("invalid StartTime: %v", err)
		}
		dur, err := parseCustomDuration(e.Duration)
		if err != nil {
			return nil, fmt.Errorf("invalid Duration: %v", err)
		}
		end, err := time.Parse(timeLayout, e.Time)
		if err != nil {
			return nil, fmt.Errorf("invalid EndTime: %v", err)
		}
		parsed = append(parsed, parsedEvent{
			Original: *e,
			NodeName: e.NodeName,
			Duration: dur,
			Start:    start,
			End:      end,
		})
	}
	// group parsed events by node_name
	grouped := make(map[string][]parsedEvent)
	for _, event := range parsed {
		// sort the events by duration
		grouped[event.NodeName] = append(grouped[event.NodeName], event)
	}
	// sort each group by duration in descending order
	for nodeName, events := range grouped {
		sort.Slice(events, func(i, j int) bool {
			return events[i].Duration > events[j].Duration
		})
		grouped[nodeName] = events
	}
	return grouped, nil
}

// findLockWaitSeries finds the longest series of lock wait events within the given time range.
func findLockWaitSeries(parsed *[]parsedEvent, startTime, endTime string) (
	lockSeries []parsedEvent, duration time.Duration, start, end time.Time, totalEvents int) {
	if len(*parsed) == 0 {
		return nil, 0, start, end, 0
	}

	startT, endT, err := parseTimeRange(startTime, endTime)
	if err != nil {
		return nil, 0, start, end, 0
	}

	processed := make(map[int]bool)
	var longestSeries []parsedEvent
	var maxDuration time.Duration
	longestStart := time.Time{}
	longestEnd := time.Time{}
	// find the longest series of lock wait events
	for {
		series, seriesStart, seriesEnd, _ := findInitialSeries(parsed, startT, endT, processed)
		if len(series) == 0 {
			break
		}
		series, seriesStart, seriesEnd = expandSeries(parsed, &series, seriesStart, seriesEnd, processed)
		duration := seriesEnd.Sub(seriesStart)
		if duration > maxDuration {
			longestSeries = series
			maxDuration = duration
			longestStart = seriesStart
			longestEnd = seriesEnd
		}
	}
	totalEvents = len(longestSeries)
	if len(longestSeries) > maxLockWaitEvents {
		longestSeries = longestSeries[:maxLockWaitEvents]
	}
	maxEventDuration := time.Duration(0)
	if len(longestSeries) > 0 {
		maxEventDuration = longestSeries[0].Duration
	}
	return longestSeries, maxEventDuration, longestStart, longestEnd, totalEvents
}

func parseTimeRange(startTime, endTime string) (startT, endT time.Time, err error) {
	startT, err = time.Parse(timeLayout, startTime)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endT, err = time.Parse(timeLayout, endTime)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return startT, endT, nil
}

// a series of lock wait events are a set of events that may overlap with each other in time.
// It finds the first event that is not processed and falls into the time range.
func findInitialSeries(parsed *[]parsedEvent, startT, endT time.Time, processed map[int]bool) (
	series []parsedEvent, seriesStart, seriesEnd time.Time, foundNew bool) {
	foundNew = false
	for i, e := range *parsed {
		if processed[i] || e.End.Before(startT) || e.End.After(endT) {
			continue
		}
		series = append(series, e)
		seriesStart = e.Start
		seriesEnd = e.End
		processed[i] = true
		foundNew = true
		break
	}
	return series, seriesStart, seriesEnd, foundNew
}

func expandSeries(parsed, series *[]parsedEvent, seriesStart, seriesEnd time.Time, processed map[int]bool) (
	updatedSeries []parsedEvent, newSeriesStart, newSeriesEnd time.Time) {
	for {
		foundOverlap := false
		for i, e := range *parsed {
			if processed[i] || !isOverlapping(&e, seriesStart, seriesEnd) {
				continue
			}
			*series = append(*series, e)
			processed[i] = true
			foundOverlap = true
			if e.Start.Before(seriesStart) {
				seriesStart = e.Start
			}
			if e.End.After(seriesEnd) {
				seriesEnd = e.End
			}
		}
		if !foundOverlap {
			break
		}
	}
	return *series, seriesStart, seriesEnd
}

// isOverlapping checks if the event overlaps with the series defined by seriesStart and seriesEnd.
// An event overlaps if its start or end time falls within the series time range,
// or if the event completely encompasses the series time range.
func isOverlapping(event *parsedEvent, seriesStart, seriesEnd time.Time) bool {
	return (event.Start.After(seriesStart) && event.Start.Before(seriesEnd)) ||
		(event.End.After(seriesStart) && event.End.Before(seriesEnd)) ||
		(event.Start.Before(seriesStart) && event.End.After(seriesEnd))
}

func findLockHoldSeries(parsed *[]parsedEvent, seriesStart time.Time) ([]parsedEvent, time.Duration) {
	// find lock hold events which falls into the wait event's time range
	var holdEvents []parsedEvent
	holdDuration := time.Duration(0)
	const maxHoldEvents = 3
	// return top up to 3 hold events
	for _, e := range *parsed {
		if e.End.After(seriesStart) && e.Start.Before(seriesStart) {
			holdEvents = append(holdEvents, e)
			holdDuration += e.Duration
		}
		if len(holdEvents) >= maxHoldEvents {
			break
		}
	}

	return holdEvents, holdDuration
}

func (opt *VClusterHealthOptions) buildLockCascadeGraph(logger vlog.Printer,
	upHosts []string) error {
	opt.LockEventCascade = make([]NodeLockEvents, 0)

	lockStartTime, err := time.Parse(timeLayout, opt.StartTime)
	if err != nil {
		return err
	}
	lockStartTime = lockStartTime.Add(lockEventsTraceBack) // find if there are any lock attempts in the previous 45 minutes
	lockStartTimeStr := lockStartTime.Format(timeLayout)
	lockAttempts, err := opt.getLockAttempts(logger, upHosts, lockStartTimeStr, opt.EndTime)
	if err != nil {
		return err
	}
	lockAttemptsParsed, err := parseAttemptsEvents(lockAttempts)
	if err != nil {
		return err
	}

	lockReleases, err := opt.getLockReleases(logger, upHosts, lockStartTimeStr, opt.EndTime)
	if err != nil {
		return err
	}

	lockReleasesParsed, err := parseReleasesEvents(lockReleases)
	if err != nil {
		return err
	}

	for nodeName, nodeAttemptsEvents := range lockAttemptsParsed {
		if len(nodeAttemptsEvents) == 0 {
			continue
		}
		nodeReleasesEvents, ok := lockReleasesParsed[nodeName]
		if !ok {
			nodeReleasesEvents = make([]parsedEvent, 0)
		}
		opt.processNodeEvents(logger, nodeName, &nodeAttemptsEvents,
			&nodeReleasesEvents, opt.StartTime, opt.EndTime)
	}

	// sort the lock events cascade by max duration in descending order
	sort.Slice(opt.LockEventCascade, func(i, j int) bool {
		return opt.LockEventCascade[i].NodeName < opt.LockEventCascade[j].NodeName
	})

	// fill session and transaction info
	opt.fillEventSessionAndTxnInfo(logger, upHosts)
	return nil
}

// processNodeEvents processes the lock events for a specific node and adds them to the lock event cascade.
// It finds the longest series of lock wait events and extracts the relevant information.
// If the series duration is less than the threshold, it skips adding the node to the cascade.
// It also extracts lock hold events related to the earliest wait locks.
func (opt *VClusterHealthOptions) processNodeEvents(logger vlog.Printer, nodeName string, nodeLockEventsParsed,
	lockReleasesParsed *[]parsedEvent, startTime, endTime string) {
	nodeLockWaitSeries, duration, seriesStart, seriesEnd, totalEvents := findLockWaitSeries(nodeLockEventsParsed, startTime, endTime)
	lockWaitEvents := extractLockWaitEvents(nodeLockWaitSeries)
	nodeLockHoldSeries, durHold := findLockHoldSeries(lockReleasesParsed, seriesStart)
	lockHoldEvents := extractLockHoldEvents(nodeLockHoldSeries)
	if duration.Seconds() <= lockWaitSeriesInSeconds {
		return
	}
	opt.LockEventCascade = append(opt.LockEventCascade, NodeLockEvents{
		NodeName:        nodeName,
		MaxDuration:     fmt.Sprintf("%0.4f", duration.Seconds()),
		WaitStartTime:   seriesStart.Format(timeLayout),
		WaitEndTime:     seriesEnd.Format(timeLayout),
		TotalWaitEvents: totalEvents,
		LockWaitEvents:  &lockWaitEvents,
		LockHoldEvents:  &lockHoldEvents,
	})

	logMessage := fmt.Sprintf("Adding node %s, max duration %0.4f lockWaitEvents %d, lockHoldEvents %d",
		nodeName, duration.Seconds()+durHold.Seconds(), len(lockWaitEvents), len(lockHoldEvents))
	logger.Info(logMessage)
}

func extractLockWaitEvents(nodeLockWaitSeries []parsedEvent) []dcLockAttempts {
	lockWaitEvents := make([]dcLockAttempts, 0)
	for _, event := range nodeLockWaitSeries {
		if originalEvent, ok := event.Original.(dcLockAttempts); ok {
			lockWaitEvents = append(lockWaitEvents, originalEvent)
		}
	}
	// sort the lock wait events by start time
	sort.Slice(lockWaitEvents, func(i, j int) bool {
		return lockWaitEvents[i].StartTime < lockWaitEvents[j].StartTime
	})
	return lockWaitEvents
}

func extractLockHoldEvents(nodeLockHoldSeries []parsedEvent) []dcLockReleases {
	lockHoldEvents := make([]dcLockReleases, 0)
	for _, event := range nodeLockHoldSeries {
		if originalEvent, ok := event.Original.(dcLockReleases); ok {
			lockHoldEvents = append(lockHoldEvents, originalEvent)
		}
	}
	// sort the lock hold events by start time
	sort.Slice(lockHoldEvents, func(i, j int) bool {
		return lockHoldEvents[i].Time < lockHoldEvents[j].Time
	})
	return lockHoldEvents
}

func (opt *VClusterHealthOptions) fillEventSessionAndTxnInfo(logger vlog.Printer, upHosts []string) {
	events := opt.LockEventCascade
	sessionSet := mapset.NewSet[string]()
	txnSet := mapset.NewSet[string]()
	// get all session id and txn id from the lock events
	for i := range events {
		event := &events[i]
		for j := range *event.LockWaitEvents {
			waitEvent := &(*event.LockWaitEvents)[j]
			sessionSet.Add(waitEvent.SessionID)
			txnSet.Add(waitEvent.TxnID)
		}
		for j := range *event.LockHoldEvents {
			holdEvent := &(*event.LockHoldEvents)[j]
			sessionSet.Add(holdEvent.SessionID)
			txnSet.Add(holdEvent.TxnID)
		}
	}

	sessionInfo, txnInfo, err := opt.getSessionTxnInfo(
		sessionSet, txnSet, logger, upHosts)
	if err != nil {
		logger.Error(err, "Failed to get session and transaction info for lock events")
		return
	}

	for i := range events {
		event := &events[i]
		// fill session and txn info for lock wait events
		for j := range *event.LockWaitEvents {
			waitEvent := &(*event.LockWaitEvents)[j]
			if s, ok := sessionInfo[waitEvent.SessionID]; ok {
				waitEvent.SessionInfo = s
			}
			if t, ok := txnInfo[waitEvent.TxnID]; ok {
				waitEvent.TxnInfo = t
			}
		}
		// fill session and txn info for lock hold events
		for j := range *event.LockHoldEvents {
			holdEvent := &(*event.LockHoldEvents)[j]
			if s, ok := sessionInfo[holdEvent.SessionID]; ok {
				holdEvent.SessionInfo = s
			}
			if t, ok := txnInfo[holdEvent.TxnID]; ok {
				holdEvent.TxnInfo = t
			}
		}
	}
	logger.Info("Filling session and transaction info for lock events",
		"session count", sessionSet.Cardinality(), "txn count", txnSet.Cardinality())
}

// DisplayLockEventsCascade prints the lock events cascade for each node in a readable format.
func (opt *VClusterHealthOptions) DisplayLockEventsCascade() {
	for _, eventNode := range opt.LockEventCascade {
		// white spaces in this block are for indentation only
		fmt.Println(eventNode.NodeName)
		fmt.Println("  Wait locks:")
		for i := range *eventNode.LockWaitEvents {
			event := (*eventNode.LockWaitEvents)[i]
			fmt.Printf("    %+v\n", event)
		}
		fmt.Printf("  Hold locks related to the earliest wait lock: %+v\n", eventNode.LockHoldEvents)
		fmt.Println("---")
	}
}
