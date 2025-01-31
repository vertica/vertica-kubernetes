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

func TestNmaGetConfigurationParameterOp_SetupRequestBody(t *testing.T) {
	op := &nmaGetConfigurationParameterOp{}

	username := "get-config-user-op"
	dbName := "get-config-db-op"
	configParameter := "get-config-param-op"
	level := "get-config-level-op"
	password := "get-config-password-op" //nolint:gosec
	useDBPassword := true

	err := op.setupRequestBody(username, dbName, configParameter, level, &password, useDBPassword)
	assert.NoError(t, err)

	expectedData := getConfigurationParameterData{
		ConfigParameter: configParameter,
		Level:           level,
		sqlEndpointData: createSQLEndpointData(username, dbName, useDBPassword, &password),
	}

	expectedBytes, _ := json.Marshal(expectedData)
	expectedRequestBody := string(expectedBytes)

	assert.Equal(t, expectedRequestBody, op.hostRequestBody)

	err = op.setupRequestBody("", dbName, configParameter, level, &password, useDBPassword)
	assert.Error(t, err)

	err = op.setupRequestBody(username, "", configParameter, level, &password, useDBPassword)
	assert.Error(t, err)

	err = op.setupRequestBody(username, dbName, configParameter, level, nil, useDBPassword)
	assert.Error(t, err)
}
