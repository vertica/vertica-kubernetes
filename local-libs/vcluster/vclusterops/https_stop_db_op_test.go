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
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMakeHTTPSStopDBOp(t *testing.T) {
	const (
		usePassword = false
		userName    = "dbadmin"
		sandboxName = ""
		mainCluster = true
		isEon       = true
		forceKill   = false
	)
	password := ""

	// if drain seconds passed as a non negative value,
	// we set the timeout to what it is and convert it to a string

	// in case of 0
	drainSeconds := 0
	httpsStopDBOp, err := makeHTTPSStopDBOp(
		usePassword,
		userName,
		&password,
		&drainSeconds,
		sandboxName,
		mainCluster,
		isEon,
		forceKill,
	)
	assert.NoError(t, err)
	assert.Equal(t, strconv.Itoa(drainSeconds), httpsStopDBOp.RequestParams["timeout"])

	// in case of 1
	drainSeconds = 1
	httpsStopDBOp, err = makeHTTPSStopDBOp(
		usePassword,
		userName,
		&password,
		&drainSeconds,
		sandboxName,
		mainCluster,
		isEon,
		forceKill,
	)
	assert.NoError(t, err)
	assert.Equal(t, strconv.Itoa(drainSeconds), httpsStopDBOp.RequestParams["timeout"])

	// if drain seconds passed as negative, we set the timeout to "",
	// which means: do not drain or wait forever
	drainSeconds = -1
	httpsStopDBOp, err = makeHTTPSStopDBOp(
		usePassword,
		userName,
		&password,
		&drainSeconds,
		sandboxName,
		mainCluster,
		isEon,
		forceKill,
	)

	assert.NoError(t, err)
	assert.Equal(t, "", httpsStopDBOp.RequestParams["timeout"])
}
