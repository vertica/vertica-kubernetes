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

package vclusterops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCaptureTimestamps(t *testing.T) {
	const (
		jan2000date = "2000-01-16 12:44:00.123456-05"
		jan2025date = "2025-01-16 12:44:00.123456-05"
		invalidDate = "2025-01-16aaa 12:44:00.123456"
	)

	options := VWorkloadCaptureOptionsFactory()

	// Positive - Start and end timestamps are valid, start time occurs before end time
	options.StartTimestamp = jan2000date
	options.EndTimestamp = jan2025date
	err := options.validateCaptureTimestamps()
	assert.NoError(t, err)

	// Negative - Invalid start timestamp
	options.StartTimestamp = invalidDate
	options.EndTimestamp = jan2025date
	err = options.validateCaptureTimestamps()
	assert.ErrorContains(t, err, "failed to parse start timestamp")

	// Negative - Empty start timestamp
	options.StartTimestamp = ""
	options.EndTimestamp = jan2025date
	err = options.validateCaptureTimestamps()
	assert.ErrorContains(t, err, "must provide start timestamp")

	// Negative - Invalid end timestamp
	options.StartTimestamp = jan2000date
	options.EndTimestamp = invalidDate
	err = options.validateCaptureTimestamps()
	assert.ErrorContains(t, err, "failed to parse end timestamp")

	// Negative - Empty end timestamp
	options.StartTimestamp = jan2000date
	options.EndTimestamp = ""
	err = options.validateCaptureTimestamps()
	assert.ErrorContains(t, err, "must provide end timestamp")

	// Negative - End timestamp occurs before start timestamp
	options.StartTimestamp = jan2025date
	options.EndTimestamp = jan2000date
	err = options.validateCaptureTimestamps()
	assert.ErrorContains(t, err, "start time must be before end time")

	// Negative - Start and end timestamps are the same
	options.StartTimestamp = jan2025date
	options.EndTimestamp = jan2025date
	err = options.validateCaptureTimestamps()
	assert.ErrorContains(t, err, "start time must be before end time")
}

func TestWorkloadFileLocation(t *testing.T) {
	options := VWorkloadCaptureOptionsFactory()

	// Positive - Valid path
	options.WorkloadFileLocation = t.TempDir() + "/test.csv"
	err := options.validateWorkloadFileLocation()
	assert.NoError(t, err)

	// Negative - empty path
	options.WorkloadFileLocation = ""
	err = options.validateWorkloadFileLocation()
	assert.ErrorContains(t, err, "must provide workload file location")
}
