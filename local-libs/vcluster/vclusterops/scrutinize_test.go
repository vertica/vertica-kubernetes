/*
 (c) Copyright [2024] Open Text.
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
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/tonglil/buflogr"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestGetHoursAgo(t *testing.T) {
	// disable this test for DST changes
	const expectedHoursAgo = 48
	isDST := time.Now().IsDST()
	isDSTPast := time.Now().Add(-2 * expectedHoursAgo).IsDST()
	isDSTFuture := time.Now().Add(2 * expectedHoursAgo).IsDST()
	if !((isDST && isDSTPast && isDSTFuture) ||
		(!isDST && !isDSTPast && !isDSTFuture)) {
		return
	}

	// use the canonical time formats from the factory
	sOptions := VScrutinizeOptionsFactory()
	const exampleVarName = "SomeTimeVar"

	// construct a mock log
	var logBuf bytes.Buffer
	logger := vlog.Printer{
		Log: buflogr.NewWithBuffer(&logBuf),
	}
	logger = logger.WithName("ScrutinizeTest")

	// test typical user time, rounding to compensate for precision loss
	// running this test in timezones that aren't an even 1hr off UTC could be a little flaky
	// because of how Round() works
	ti := time.Now().Round(time.Hour).Add(-1 * expectedHoursAgo * time.Hour)
	// allow alternate time in case this test runs around xx:30:00 exactly
	// 15 minutes of leeway means unless this thread sleeps for >15 minutes between time.Now() calls, it's reliable
	const leewayMinutes = 15
	altHoursAgo := expectedHoursAgo
	tiAlt := time.Now().Round(0).Add(-1 * leewayMinutes * time.Minute).Round(time.Hour).Add(-1 * expectedHoursAgo * time.Hour)
	if !ti.Equal(tiAlt) {
		// example: 12:29:29 rounds to 12:00:00, but 12:30:00 rounds to 13:00:00, so hours ago increases by 1
		altHoursAgo--
	}

	for _, format := range sOptions.timeFormats {
		hoursAgo, err := sOptions.getHoursAgo(ti.Format(format.Layout), exampleVarName, logger)
		assert.NoError(t, err)
		isHoursAgoExpected := (expectedHoursAgo == hoursAgo) || (altHoursAgo == hoursAgo)
		assert.True(t, isHoursAgoExpected)
	}

	// test future time
	const negativeHoursAgo = -24
	const zeroHoursAgo = 0
	ti = time.Now().Round(time.Hour).Add(-1 * negativeHoursAgo * time.Hour)

	for _, format := range sOptions.timeFormats {
		hoursAgo, err := sOptions.getHoursAgo(ti.Format(format.Layout), exampleVarName, logger)
		assert.NoError(t, err)
		assert.Equal(t, zeroHoursAgo, hoursAgo)
		assert.Contains(t, logBuf.String(), "Provided time is or rounds to a future time.  Using 0 instead.")
	}

	// test bad time string
	ti = time.Now().Round(time.Hour)
	timeStr := ti.Format(time.UnixDate)
	_, err := sOptions.getHoursAgo(timeStr, exampleVarName, logger)
	expectedErrStr := fmt.Sprintf("unable to parse time '%s' according to allowed format %s", timeStr, ScrutinizeHelpTimeFormatDesc)
	assert.ErrorContains(t, err, expectedErrStr)
}

func TestSetLogAgeRange(t *testing.T) {
	// construct a mock log
	var logBuf bytes.Buffer
	logger := vlog.Printer{
		Log: buflogr.NewWithBuffer(&logBuf),
	}
	logger = logger.WithName("ScrutinizeTest")

	// the default of 24 hours log age comes from the CLI frontend, not the factory
	sOptions := VScrutinizeOptionsFactory()
	sOptions.LogAgeHours = ScrutinizeLogMaxAgeHoursDefault

	// test with default values
	err := sOptions.setLogAgeRange(logger)
	assert.NoError(t, err)
	assert.Equal(t, ScrutinizeLogMaxAgeHoursDefault, sOptions.logAgeMaxHours)
	assert.Zero(t, sOptions.logAgeMinHours)
	assert.Contains(t, logBuf.String(), "Archived log time range set")

	// test with LogAgeHours set
	const customLogAgeHours = 48
	sOptions.LogAgeHours = customLogAgeHours
	err = sOptions.setLogAgeRange(logger)
	assert.NoError(t, err)
	assert.Equal(t, customLogAgeHours, sOptions.logAgeMaxHours)
	assert.Contains(t, logBuf.String(), "Archived log time range set")

	// test with LogAgeOldestTime set
	tiPast, err := time.Parse("2006", "1995")
	if err != nil {
		panic(err)
	}
	assert.NotEmpty(t, sOptions.timeFormats)
	sOptions.LogAgeOldestTime = tiPast.Format(sOptions.timeFormats[0].Layout)
	err = sOptions.setLogAgeRange(logger)
	assert.NoError(t, err)
	assert.Less(t, ScrutinizeLogMaxAgeHoursDefault, sOptions.logAgeMaxHours)
	assert.Contains(t, logBuf.String(), "Archived log time range set")

	// test with LogAgeNewestTime set
	tiLessPast, err := time.Parse("2006", "2000")
	if err != nil {
		panic(err)
	}
	sOptions.LogAgeNewestTime = tiLessPast.Format(sOptions.timeFormats[0].Layout)
	err = sOptions.setLogAgeRange(logger)
	assert.NoError(t, err)
	assert.Less(t, ScrutinizeLogMaxAgeHoursDefault, sOptions.logAgeMaxHours)
	assert.Less(t, ScrutinizeLogMaxAgeHoursDefault, sOptions.logAgeMinHours)
	assert.Less(t, sOptions.logAgeMinHours, sOptions.logAgeMaxHours)
	assert.Contains(t, logBuf.String(), "Archived log time range set")

	// test with incompatible values
	sOptions.LogAgeOldestTime = tiLessPast.Format(sOptions.timeFormats[0].Layout)
	sOptions.LogAgeNewestTime = tiPast.Format(sOptions.timeFormats[0].Layout)
	err = sOptions.setLogAgeRange(logger)
	assert.ErrorContains(t, err, "invalid time range: max log age cannot be less than min log age")
	assert.Contains(t, logBuf.String(), "invalid log age range")
}
