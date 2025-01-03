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

package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseTime(t *testing.T) {
	// unix date but without timestamp
	unixDateNoTzFormat := TimeFormat{Layout: "Mon Jan _2 15:04:05 2006", UseLocalTZ: true}
	unixDateFormat := TimeFormat{Layout: time.UnixDate}

	// use time with and without tz
	refTime := time.Now().Truncate(time.Second)
	tString := refTime.Format(unixDateFormat.Layout)
	tNoTzString := refTime.Format(unixDateNoTzFormat.Layout)
	tBadString := refTime.Format(time.Stamp)

	// test with no formats
	_, err := ParseTime(tString, []TimeFormat{})
	assert.Error(t, err)

	// test with one format where string matches
	formats := []TimeFormat{unixDateFormat}
	ti, err := ParseTime(tString, formats)
	assert.NoError(t, err)
	isTimeEqual := refTime.Equal(ti)
	assert.True(t, isTimeEqual)

	// test with two formats where string matches 2nd
	formats = []TimeFormat{unixDateNoTzFormat, unixDateFormat}
	ti, err = ParseTime(tString, formats)
	assert.NoError(t, err)
	isTimeEqual = refTime.Equal(ti)
	assert.True(t, isTimeEqual)

	// test where string matches neither format
	formats = []TimeFormat{unixDateNoTzFormat, unixDateFormat}
	_, err = ParseTime(tBadString, formats)
	assert.Error(t, err)

	// test where string matches no timezone format and replaces UTC with local tz
	formats = []TimeFormat{unixDateNoTzFormat}
	ti, err = ParseTime(tNoTzString, formats)
	assert.NoError(t, err)
	isTimeEqual = refTime.Equal(ti)
	assert.True(t, isTimeEqual)

	// test where string matches no timezone format and doesn't replace UTC with local tz
	unixDateUTCOnlyFormat := TimeFormat{Layout: unixDateNoTzFormat.Layout}
	formats = []TimeFormat{unixDateUTCOnlyFormat}
	ti, err = ParseTime(tNoTzString, formats)
	assert.NoError(t, err)
	isTimeEqual = refTime.Equal(ti)
	// special case for running this test in local UTC or equivalent
	isLocalUTC := refTime.Format(unixDateNoTzFormat.Layout) == refTime.UTC().Format(unixDateNoTzFormat.Layout)
	assert.Equal(t, isTimeEqual, isLocalUTC)
}

// HoursAgo rounds to the nearest hour, so unless there is so much stress
// that a thread sleeps for >30 minutes, this is reliable
func TestHoursAgo(t *testing.T) {
	// test same time
	hoursAgo, err := HoursAgo(time.Now())
	assert.NoError(t, err)
	assert.Zero(t, hoursAgo)

	// test time in past
	hoursAgo, err = HoursAgo(time.Now().Add(-1 * time.Hour))
	assert.NoError(t, err)
	assert.Equal(t, hoursAgo, 1)

	// test time in future
	hoursAgo, err = HoursAgo(time.Now().Add(1 * time.Hour))
	assert.NoError(t, err)
	assert.Equal(t, hoursAgo, -1)
}
