package vclusterops

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindSpecifiedRestorePoint(t *testing.T) {
	archiveVal := "archive1"
	idVal := "id1"
	indexVal := 0
	options := VReviveDatabaseOptions{
		RestorePoint: RestorePointPolicy{
			Archive: archiveVal,
			ID:      idVal,
			Index:   indexVal,
		},
	}

	allRestorePoints := []RestorePoint{
		{Archive: "archive1", ID: "id1", Index: 1},
		{Archive: "archive2", ID: "id2", Index: 1},
		{Archive: "archive2", ID: "id3", Index: 2},
		{Archive: "archive1", ID: "id3", Index: 2},
		{Archive: "archive1", ID: "id3", Index: 3},
	}

	// Test case: Found a single matching restore point
	expectedID := idVal
	actualID, err := options.findSpecifiedRestorePoint(allRestorePoints)
	assert.NoError(t, err)
	assert.Equal(t, expectedID, actualID)

	options.RestorePoint.Index = 2
	options.RestorePoint.ID = ""
	expectedID = "id3"
	actualID, err = options.findSpecifiedRestorePoint(allRestorePoints)
	assert.NoError(t, err)
	assert.Equal(t, expectedID, actualID)

	// Test case: Found multiple matching restore points
	options.RestorePoint.Index = 0
	options.RestorePoint.ID = expectedID
	_, err = options.findSpecifiedRestorePoint(allRestorePoints)
	expectedErr := fmt.Errorf("found 2 restore points instead of 1: " +
		"[{Archive:archive1 ID:id3 Index:2 Timestamp: VerticaVersion:} " +
		"{Archive:archive1 ID:id3 Index:3 Timestamp: VerticaVersion:}]")
	assert.EqualError(t, err, expectedErr.Error())

	// Test case: No matching restore points found
	options.RestorePoint.Archive = "archive3"
	_, err = options.findSpecifiedRestorePoint(allRestorePoints)
	expectedErr = &ReviveDBRestorePointNotFoundError{Archive: "archive3", InvalidID: "id3"}
	assert.EqualError(t, err, expectedErr.Error())
}
