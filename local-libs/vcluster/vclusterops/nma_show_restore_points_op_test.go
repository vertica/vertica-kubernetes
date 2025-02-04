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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

const archName = `"archive_name":"`
const archID = `"archive_id":"`
const archIndex = `"archive_index":"`

func TestShowRestorePointsRequestBody(t *testing.T) {
	const hostName = "host1"
	const dbName = "testDB"
	const communalLocation = "/communal"
	archiveName := "test_name"
	archiveID := "test_ID"
	archiveIndex := "test_index"
	startTimestamp := "2006-01-02 15:04:05"
	endTimestamp := "2006-01-02 15:04:06"
	op := makeNMAShowRestorePointsOp(vlog.Printer{}, []string{hostName}, dbName, communalLocation, nil)

	requestBody, err := op.setupRequestBody()
	assert.NoError(t, err)
	assert.Len(t, requestBody, 1)
	assert.Contains(t, requestBody, hostName)
	hostReq := requestBody[hostName]
	assert.Contains(t, hostReq, `"communal_location":"`+communalLocation+`"`)
	assert.Contains(t, hostReq, `"db_name":"`+dbName+`"`)

	op = makeNMAShowRestorePointsOpWithFilterOptions(vlog.Printer{}, []string{hostName},
		dbName, communalLocation, nil, &ShowRestorePointFilterOptions{
			ArchiveName:    archiveName,
			ArchiveID:      archiveID,
			ArchiveIndex:   archiveIndex,
			StartTimestamp: startTimestamp,
			EndTimestamp:   endTimestamp,
		})

	requestBody, err = op.setupRequestBody()
	assert.NoError(t, err)
	assert.Len(t, requestBody, 1)
	assert.Contains(t, requestBody, hostName)
	hostReq = requestBody[hostName]
	assert.Contains(t, hostReq, archName+archiveName+`"`)
	assert.Contains(t, hostReq, archID+archiveID+`"`)
	assert.Contains(t, hostReq, archIndex+archiveIndex+`"`)
	assert.Contains(t, hostReq, `"start_timestamp":"`+startTimestamp+`"`)
	assert.Contains(t, hostReq, `"end_timestamp":"`+endTimestamp+`"`)

	op = makeNMAShowRestorePointsOpWithFilterOptions(vlog.Printer{}, []string{hostName},
		dbName, communalLocation, nil, &ShowRestorePointFilterOptions{
			ArchiveName:  archiveName,
			ArchiveID:    archiveID,
			ArchiveIndex: archiveIndex,
		})

	requestBody, err = op.setupRequestBody()
	assert.NoError(t, err)
	assert.Len(t, requestBody, 1)
	assert.Contains(t, requestBody, hostName)
	hostReq = requestBody[hostName]
	assert.Contains(t, hostReq, archName+archiveName+`"`)
	assert.Contains(t, hostReq, archID+archiveID+`"`)
	assert.Contains(t, hostReq, archIndex+archiveIndex+`"`)
	assert.NotContains(t, hostReq, `"start_timestamp"`)
	assert.NotContains(t, hostReq, `"end_timestamp"`)
}
