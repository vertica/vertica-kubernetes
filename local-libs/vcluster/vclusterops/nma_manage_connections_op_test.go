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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNmaManageConnectionsOp_SetupRequestBody(t *testing.T) {
	op := &nmaManageConnectionsOp{}
	op.action = ActionRedirect

	username := "draining-test-user-op"
	dbName := "draining-test-db-op"
	subclusterName := "draining-test-subcluster-op"
	redirectHostname := "draining-test-redirect-op"
	password := "draining-test-password-op"
	useDBPassword := true

	err := op.setupRequestBody(username, dbName, subclusterName, redirectHostname, &password, useDBPassword)
	assert.NoError(t, err)

	expectedData := manageConnectionsData{
		SubclusterName:   subclusterName,
		RedirectHostname: redirectHostname,
		sqlEndpointData:  createSQLEndpointData(username, dbName, useDBPassword, &password),
	}

	expectedBytes, _ := json.Marshal(expectedData)
	expectedRequestBody := string(expectedBytes)

	assert.Equal(t, expectedRequestBody, op.hostRequestBody)

	err = op.setupRequestBody("", dbName, subclusterName, redirectHostname, &password, useDBPassword)
	assert.Error(t, err)

	err = op.setupRequestBody(username, "", subclusterName, redirectHostname, &password, useDBPassword)
	assert.Error(t, err)

	err = op.setupRequestBody(username, dbName, subclusterName, redirectHostname, nil, useDBPassword)
	assert.Error(t, err)
}
