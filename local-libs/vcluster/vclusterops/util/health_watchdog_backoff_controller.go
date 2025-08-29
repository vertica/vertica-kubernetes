/*
(c) Copyright [2023-2025] Open Text.
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
package util

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

// TODO: Move TLS backoff code from vops layer to vcluster_server

// ErrTLSBackedOff is a special error for the backed-off state.
type ErrTLSBackedOff struct {
	RetryAfter time.Duration // RetryAfter is wait duration before the next attempt.
}

func (e *ErrTLSBackedOff) Error() string {
	return "operation is in a backed-off state due to a SSL/TLS error"
}

const (
	defaultPollingInterval = 5 * time.Second

	// Polling intervals
	initialPollingInterval = 5 * time.Second
	mediumPollingInterval  = 5 * time.Minute
	longPollingInterval    = 10 * time.Minute

	// Duration thresholds
	initialBackoffDuration = 5 * time.Minute
	mediumBackoffDuration  = 65 * time.Minute // 5 minutes + 1 hour
)

// Controller Registry
var (
	controllers  = make(map[string]*BackoffController)
	controllerMu sync.Mutex
)

// GetBackoffController gets or creates a dedicated controller for a named service.
func GetBackoffController(logger vlog.Printer, serviceName string) *BackoffController {
	controllerMu.Lock()
	defer controllerMu.Unlock()

	if controller, ok := controllers[serviceName]; ok {
		return controller
	}

	logPrefix := fmt.Sprintf("[%s]", serviceName)
	newController := NewBackoffController(logger, logPrefix)
	controllers[serviceName] = newController
	return newController
}

// BackoffController holds the state for the back-off logic.
type BackoffController struct {
	isErrorActive   bool
	errorTime       time.Time
	currentInterval time.Duration
	logPrefix       string
	mu              sync.Mutex
	logger          vlog.Printer
}

// NewBackoffController creates a new controller with a given prefix for its logs.
func NewBackoffController(logger vlog.Printer, logPrefix string) *BackoffController {
	return &BackoffController{
		logger:          logger,
		logPrefix:       logPrefix,
		currentInterval: defaultPollingInterval,
	}
}

// GetInterval returns the current polling interval.
func (bc *BackoffController) GetInterval() time.Duration {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.currentInterval
}

// IsBackedOff returns true if the controller is currently in a backed-off state
// due to a persistent error.
func (bc *BackoffController) IsBackedOff() bool {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.isErrorActive
}

// UpdateState processes an error and updates the back-off state and interval.
func (bc *BackoffController) UpdateState(err error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if err != nil {
		// On the first error detection, log a warning and start the timer.
		if IsTLSError(err) {
			if !bc.isErrorActive {
				bc.logger.PrintWarning("%s: Data collection failing due to SSL/TLS error. Will retry every %v for 5 minutes before backing off.",
					bc.logPrefix, defaultPollingInterval)
				bc.isErrorActive = true
				bc.errorTime = time.Now()
			}

			// Calculate the next polling interval based on the total error duration.
			errorDuration := time.Since(bc.errorTime)
			var nextInterval time.Duration

			if errorDuration <= initialBackoffDuration {
				// First 5 minutes: poll frequently.
				nextInterval = initialPollingInterval
			} else if errorDuration <= mediumBackoffDuration {
				// Next hour: poll less frequently
				nextInterval = mediumPollingInterval
			} else {
				// After that: poll infrequently.
				nextInterval = longPollingInterval
			}

			if bc.currentInterval != nextInterval {
				bc.currentInterval = nextInterval
				bc.logger.PrintWarning("%s: SSL/TLS error persists. Reducing polling frequency to %v.", bc.logPrefix, nextInterval)
			}
			return
		}

		// For all other non-TLS errors.
		bc.logger.PrintError("%s: Failed to fetch health watchdog data: %v", bc.logPrefix, err)
		return
	}

	// Once TLS is enabled.
	if bc.isErrorActive {
		bc.logger.PrintInfo("%s: SSL/TLS is enabled. Resuming normal polling interval of %v.", bc.logPrefix, defaultPollingInterval)
		bc.isErrorActive = false
		// Reset to the normal interval.
		bc.currentInterval = defaultPollingInterval
	}
}

// IsTLSError checks if an error is related to SSL/TLS configuration
func IsTLSError(err error) bool {
	if err == nil {
		return false
	}
	errorMsg := strings.ToLower(err.Error())
	return strings.Contains(errorMsg, "ssl") ||
		strings.Contains(errorMsg, "tls") ||
		strings.Contains(errorMsg, "certificate") ||
		strings.Contains(errorMsg, "unauthorized") ||
		strings.Contains(errorMsg, "ssl/tls is not enabled")
}
