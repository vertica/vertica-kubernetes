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
	"time"
)

const (
	OneSecond                = 1
	OneMinute                = 60 * OneSecond
	StopDBTimeout            = 5 * OneMinute
	StartupPollingTimeout    = 5 * OneMinute
	StopPollingTimeout       = 5 * OneMinute
	ScrutinizePollingTimeout = -1 * OneMinute // no timeout
	PollingInterval          = 3 * OneSecond
)

type statePoller interface {
	getPollingTimeout() int
	shouldStopPolling() (bool, error)
	runExecute(execContext *opEngineExecContext) error
}

// pollState is a helper function to poll state for all ops that implement the StatePoller interface.
// If poller.getPollingTimeout() returns a value < 0, pollState will poll forever.
func pollState(poller statePoller, execContext *opEngineExecContext) error {
	startTime := time.Now()
	timeout := poller.getPollingTimeout()
	duration := time.Duration(timeout) * time.Second
	count := 0
	needTimeout := true
	if timeout <= 0 {
		needTimeout = false
	}

	for endTime := startTime.Add(duration); ; {
		if needTimeout && time.Now().After(endTime) {
			break
		}

		if count > 0 {
			time.Sleep(PollingInterval * time.Second)
		}

		shouldStopPoll, err := poller.shouldStopPolling()
		if err != nil {
			return err
		}

		if shouldStopPoll {
			return nil
		}

		if err := poller.runExecute(execContext); err != nil {
			return err
		}

		count++
	}

	return fmt.Errorf("reached polling timeout of %d seconds", timeout)
}
