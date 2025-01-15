package vclusterops

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

const defaultStartTime = " 00:00:00"
const defaultEndTime = " 23:59:59"

func TestShowRestorePointFilterOptions_ValidateAndStandardizeTimestampsIfAny(t *testing.T) {
	// Test case 1: No validation needed
	filterOptions := ShowRestorePointFilterOptions{
		StartTimestamp: "",
		EndTimestamp:   "",
	}
	err := filterOptions.ValidateAndStandardizeTimestampsIfAny()
	assert.NoError(t, err)

	// Test case 2: Invalid start timestamp
	startTimestamp := "invalid_start_timestamp"
	filterOptions = ShowRestorePointFilterOptions{
		StartTimestamp: startTimestamp,
		EndTimestamp:   "",
	}
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	expectedErr := fmt.Errorf("start timestamp %q is invalid;", startTimestamp)
	assert.ErrorContains(t, err, expectedErr.Error())

	// Test case 3: Invalid end timestamp
	endTimestamp := "invalid_end_timestamp"
	filterOptions = ShowRestorePointFilterOptions{
		StartTimestamp: "",
		EndTimestamp:   endTimestamp,
	}
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	expectedErr = fmt.Errorf("end timestamp %q is invalid;", endTimestamp)
	assert.ErrorContains(t, err, expectedErr.Error())

	const earlierDate = "2022-01-01"
	const laterDate = "2022-01-02"

	// Test case 4: Valid start and end timestamps
	startTimestamp = earlierDate + defaultStartTime
	endTimestamp = laterDate + defaultStartTime
	filterOptions = ShowRestorePointFilterOptions{
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	}
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	assert.NoError(t, err)

	filterOptions.StartTimestamp = earlierDate
	filterOptions.EndTimestamp = laterDate
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	assert.NoError(t, err)
	assert.Equal(t, earlierDate+" 00:00:00.000000000", filterOptions.StartTimestamp)
	assert.Equal(t, laterDate+" 23:59:59.999999999", filterOptions.EndTimestamp)

	filterOptions.StartTimestamp = earlierDate
	filterOptions.EndTimestamp = earlierDate
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	assert.NoError(t, err)

	filterOptions.StartTimestamp = earlierDate
	filterOptions.EndTimestamp = laterDate + defaultEndTime
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	assert.NoError(t, err)

	filterOptions.StartTimestamp = earlierDate + " 01:01:01.010101010"
	filterOptions.EndTimestamp = laterDate
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	assert.NoError(t, err)

	filterOptions.StartTimestamp = earlierDate + defaultEndTime
	filterOptions.EndTimestamp = earlierDate + " 23:59:59.123456789"
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	assert.NoError(t, err)

	// Test case 5: Start timestamp after end timestamp
	filterOptions = ShowRestorePointFilterOptions{
		StartTimestamp: endTimestamp,
		EndTimestamp:   startTimestamp,
	}
	err = filterOptions.ValidateAndStandardizeTimestampsIfAny()
	assert.EqualError(t, err, "start timestamp must be before end timestamp")
}
