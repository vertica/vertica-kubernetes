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
	nodeName   string
	lockEvents []*dcLockAttempts
}

func (options *VClusterHealthOptions) buildLockCascadeGraph(logger vlog.Printer,
	upHosts []string) error {
	lockAttempts, err := options.getLockAttempts(logger, upHosts,
		options.StartTime, options.EndTime)
	if err != nil {
		return err
	}

	// find the earliest lock waiting event in each node
	if len(*lockAttempts) == 0 {
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
	// then request /v1/dc/lock-attempts using the node_name, and the new times
	for _, event := range nodeLockStartMap {
		e := options.recursiveTraceLocks(logger, upHosts, event, 1)
		if e != nil {
			return e
		}
	}

	// sort the cascade result by the start time
	// node with earlier time goes first
	sort.Slice(options.LockEventCascade, func(i, j int) bool {
		return options.LockEventCascade[i].lockEvents[len(options.LockEventCascade[i].lockEvents)-1].StartTime <
			options.LockEventCascade[j].lockEvents[len(options.LockEventCascade[i].lockEvents)-1].StartTime
	})

	for _, item := range options.LockEventCascade {
		fmt.Printf("---\n%s\n", item.nodeName)
		for _, event := range item.lockEvents {
			fmt.Printf("    %+v\n", event)
		}

		earliestEvent := item.lockEvents[len(item.lockEvents)-1]
		e := options.getLockReleases(logger, upHosts, earliestEvent.StartTime, earliestEvent.Time)
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
	nmaLockAttemptsOp, err := makeNMALockAttemptsOp(upHosts, startTime, endTime,
		"", resultLimit)
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
	event *dcLockAttempts, level int) error {
	logger.Info("Lock wait event", "node name", event.NodeName, "event", event)

	var lastElem nodeLockEvents
	count := len(options.LockEventCascade)
	if count > 0 {
		lastElem = options.LockEventCascade[count-1]
	}
	// if node exists in the result, append new event into it
	// otherwise, create a new node elem
	if count > 0 && lastElem.nodeName == event.NodeName {
		lastElem.lockEvents = append(lastElem.lockEvents, event)
		options.LockEventCascade[count-1] = lastElem
	} else {
		var locksInNode nodeLockEvents
		locksInNode.nodeName = event.NodeName
		locksInNode.lockEvents = append(locksInNode.lockEvents, event)
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

	nmaLockAttemptsOp, err := makeNMALockAttemptsOp(upHosts, priorTimeStr, event.StartTime,
		event.NodeName, resultLimit)
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
	if len(*lockAttemptList) == 0 {
		return nil
	}

	// pick the event that has the earliest start_time, then keep tracing
	// here we pick the first element as the result is sorted by start_time already
	eventWithEarliestStartTime := (*clusterOpEngine.execContext.dcLockAttemptsList)[0]

	return options.recursiveTraceLocks(logger, upHosts, &eventWithEarliestStartTime, level+1)
}

func (options *VClusterHealthOptions) getLockReleases(logger vlog.Printer, upHosts []string,
	startTime, endTime string) error {
	var instructions []clusterOp

	const resultLimit = 5
	nmaLockAttemptsOp, err := makeNMALockReleasesOp(upHosts, startTime, endTime,
		"", resultLimit)
	if err != nil {
		return err
	}
	instructions = append(instructions, &nmaLockAttemptsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &options.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return fmt.Errorf("fail to get lock waiting events, %w", err)
	}

	fmt.Println("AAAAAAAAAAA", *clusterOpEngine.execContext.dcLockReleasesList)

	return nil
}
