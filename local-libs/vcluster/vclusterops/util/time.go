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
	"errors"
	"fmt"
	"math"
	"time"
)

// TimeFormat defines a format for parsing time, including whether local timezone should be
// assumed.  This is useful for processing user input when parsing time formatted without
// a timezone, as golang assumes UTC despite assuming local timezone being more intuitive.
type TimeFormat struct {
	Layout     string
	UseLocalTZ bool
}

// ParseTime attempts to parse an input time string according to a series of provided
// valid formats.  Additionally, it supports assuming or overriding with local timezone,
// useful in the case where the format string does not have a timezone element.
func ParseTime(timeString string, formats []TimeFormat) (t time.Time, allErrs error) {
	if len(formats) == 0 {
		allErrs = fmt.Errorf("no formats provided")
	}

	for _, format := range formats {
		t, err := time.Parse(format.Layout, timeString)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}
		if format.UseLocalTZ {
			t = applyLocalTimezone(t)
		}
		return t, nil
	}
	return time.Time{}, allErrs
}

// HoursAgo calculates the hours from the input time to the current time, erroring
// in case of over/underflow.
func HoursAgo(t time.Time) (int, error) {
	timeAgo := time.Since(t)
	hoursAgo := math.Round(timeAgo.Hours())
	if hoursAgo > float64(math.MaxInt32) ||
		hoursAgo < float64(math.MinInt32) {
		return 0, fmt.Errorf("'%g' hours ago larger than supported by int arg type", timeAgo.Hours())
	}
	return int(hoursAgo), nil
}

// applyLocalTimezone takes a timestamp, discards the original timezone, and applies the local
// timezone as if the other time.Time values were specified for that time zone.  For example,
// given 5:00 PM UTC and a local time zone EST, the returned time will be 5:00 PM EST rather
// than 12:00 PM EST.
func applyLocalTimezone(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(),
		t.Second(), t.Nanosecond(), time.Local)
}
