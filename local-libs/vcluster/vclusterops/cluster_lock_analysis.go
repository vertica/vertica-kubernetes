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
	"time"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type nodeLockEvents struct {
	NodeName       string
	LockWaitEvents []*dcLockAttempts `json:"wait_locks"`
	LockHoldEvents *[]dcLockReleases `json:"hold_locks"` // hold locks related to earliest wait locks
}

func (options *VClusterHealthOptions) buildLockCascadeGraph(logger vlog.Printer,
	upHosts []string) error {
	lockAttempts, err := options.getLockAttempts(logger, upHosts,
		options.StartTime, options.EndTime)
	if err != nil {
		return err
	}

	// find the earliest lock waiting event in each node
	if lockAttempts == nil || len(*lockAttempts) == 0 {
		return nil
	}

	vlog.DisplayColorInfo("Building cascade graph for lock events")

	nodeLockStartMap := make(map[string]*dcLockAttempts)
	for i := range *lockAttempts {
		event := (*lockAttempts)[i]
		nodeName := event.NodeName
		if _, exists := nodeLockStartMap[nodeName]; exists {
			if event.StartTime < nodeLockStartMap[nodeName].StartTime {
				nodeLockStartMap[nodeName] = &event
			}
		} else {
			nodeLockStartMap[event.NodeName] = &event
		}
	}

	// recursively track the earliest lock wait event in each node
	// i.e., given a detected lock wait event:
	// - use its start_time as the new end_time
	// - start_time - tracebackTime as the new start_time
	// then request /v1/dc/lock-attempts using the node_name and the new times
	for _, event := range nodeLockStartMap {
		e := options.recursiveTraceLocks(logger, upHosts, event, 1)
		if e != nil {
			return e
		}
	}

	// sort the cascade result by the start time and duration
	// node with earlier time and longer duration goes first
	sort.Slice(options.LockEventCascade, func(i, j int) bool {
		event1 := options.LockEventCascade[i].LockWaitEvents[len(options.LockEventCascade[i].LockWaitEvents)-1]
		event2 := options.LockEventCascade[j].LockWaitEvents[len(options.LockEventCascade[j].LockWaitEvents)-1]
		if event1.StartTime != event2.StartTime {
			return event1.StartTime < event2.StartTime
		}
		return event1.Duration > event2.Duration
	})

	// for each node's result, we pick its earliest lock wait event,
	// then find out the event's related/correlated lock hold events
	for i, item := range options.LockEventCascade {
		earliestEvent := item.LockWaitEvents[len(item.LockWaitEvents)-1]
		e := options.getLockReleases(logger, upHosts, earliestEvent.NodeName,
			earliestEvent.StartTime, earliestEvent.Time, i)
		if e != nil {
			return e
		}
	}

	return nil
}

func (options *VClusterHealthOptions) getLockAttempts(logger vlog.Printer, upHosts []string,
	startTime, endTime string) (lockAttempts *[]dcLockAttempts, err error) {
	var instructions []clusterOp

	const resultLimit = 1024
	nmaLockAttemptsOp, err := makeNMALockAttemptsOp(upHosts, options.DatabaseOptions.UserName,
		startTime, endTime, "" /*node name*/, resultLimit)
	if err != nil {
		return nil, err
	}
	instructions = append(instructions, &nmaLockAttemptsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return nil, fmt.Errorf("fail to get lock waiting events, %w", err)
	}

	return clusterOpEngine.execContext.dcLockAttemptsList, nil
}

func (options *VClusterHealthOptions) recursiveTraceLocks(logger vlog.Printer, upHosts []string,
	event *dcLockAttempts, depth int) error {
	logger.Info("Lock wait event", "node name", event.NodeName, "event", event)

	// set a max depth to avoid exhaustive recursion
	if depth > maxDepth {
		return nil
	}

	var lastElem nodeLockEvents
	count := len(options.LockEventCascade)
	if count > 0 {
		lastElem = options.LockEventCascade[count-1]
	}
	// if node exists in the result, append new event into it
	// otherwise, create a new node elem
	if count > 0 && lastElem.NodeName == event.NodeName {
		lastElem.LockWaitEvents = append(lastElem.LockWaitEvents, event)
		options.LockEventCascade[count-1] = lastElem
	} else {
		var locksInNode nodeLockEvents
		locksInNode.NodeName = event.NodeName
		locksInNode.LockWaitEvents = append(locksInNode.LockWaitEvents, event)
		options.LockEventCascade = append(options.LockEventCascade, locksInNode)
	}

	var instructions []clusterOp

	const resultLimit = 5
	const tracebackTime = 60 * 10 // seconds

	start, err := time.Parse(timeLayout, event.StartTime)
	if err != nil {
		return err
	}
	priorTimePoint := start.Add(-time.Duration(tracebackTime) * time.Second)
	priorTimeStr := priorTimePoint.Format(timeLayout)

	nmaLockAttemptsOp, err := makeNMALockAttemptsOp(upHosts, options.DatabaseOptions.UserName,
		priorTimeStr, event.StartTime, event.NodeName, resultLimit)
	if err != nil {
		return err
	}
	instructions = append(instructions, &nmaLockAttemptsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return fmt.Errorf("fail to get lock waiting events, %w", err)
	}

	lockAttemptList := clusterOpEngine.execContext.dcLockAttemptsList

	// stop recursion if no more events found
	if len(*lockAttemptList) == 0 {
		return nil
	}

	// pick the event that has the earliest start_time, then keep tracing
	// here we pick the first element as the result is sorted by start_time already
	eventWithEarliestStartTime := (*lockAttemptList)[0]

	return options.recursiveTraceLocks(logger, upHosts, &eventWithEarliestStartTime, depth+1)
}

func (options *VClusterHealthOptions) getLockReleases(logger vlog.Printer, upHosts []string,
	nodeName, startTime, endTime string, cascadeIndex int) error {
	var instructions []clusterOp

	const resultLimit = 5
	nmaLockAttemptsOp, err := makeNMALockReleasesOp(upHosts, options.DatabaseOptions.UserName,
		startTime, endTime, nodeName, resultLimit)
	if err != nil {
		return err
	}
	instructions = append(instructions, &nmaLockAttemptsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return fmt.Errorf("fail to get lock waiting events, %w", err)
	}

	if clusterOpEngine.execContext.dcLockReleasesList != nil {
		options.LockEventCascade[cascadeIndex].LockHoldEvents = clusterOpEngine.execContext.dcLockReleasesList
	}

	return nil
}
