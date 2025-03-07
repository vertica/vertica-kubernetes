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

	"fmt"

	"github.com/stretchr/testify/assert"
)

const (
	validQuery1      = "SELECT * FROM query_requests;"
	validQuery2      = "SELECT name, age FROM people;"
	validTimestamp1  = "2020-01-02T15:04:05.999999-07:00"
	validTimestamp2  = "2023-02-03T16:05:06.999999-08:00"
	node1Name        = "node1"
	invalidTimestamp = "2020-01-02 15:04:05.99"
)

func TestValidateOriginalWorkloadData(t *testing.T) {
	replayData := workloadReplayData{}
	replayRequestData := nmaWorkloadReplayRequestData{}

	op, err := makeNMAWorkloadReplayOp(nil, false, nil, &replayRequestData, &replayData)
	assert.NoError(t, err)

	// Positive - valid data
	replayData.originalWorkloadData = []workloadQuery{
		{
			Request:        validQuery1,
			StartTimestamp: validTimestamp1,
			NodeName:       node1,
		},
		{
			Request:        validQuery2,
			StartTimestamp: validTimestamp2,
			NodeName:       node1Name,
		},
	}
	fmt.Print("here 2")
	err = op.validateOriginalWorkloadData()
	assert.NoError(t, err)

	// Negative - invalid timestamp format
	replayData.originalWorkloadData = []workloadQuery{
		{
			Request:        validQuery1,
			StartTimestamp: invalidTimestamp,
			NodeName:       node1Name,
		},
	}
	err = op.validateOriginalWorkloadData()
	assert.ErrorContains(t, err, "invalid start timestamp at workload query index 0")

	// Negative - empty query
	replayData.originalWorkloadData = []workloadQuery{
		{
			Request:        "",
			StartTimestamp: validTimestamp1,
			NodeName:       node1Name,
		},
	}
	err = op.validateOriginalWorkloadData()
	assert.ErrorContains(t, err, "empty request at index 0")

	// Negative - empty node name
	replayData.originalWorkloadData = []workloadQuery{
		{
			Request:        validQuery1,
			StartTimestamp: validTimestamp1,
			NodeName:       "",
		},
	}
	err = op.validateOriginalWorkloadData()
	assert.ErrorContains(t, err, "empty node name at index 0")
}
