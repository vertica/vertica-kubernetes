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

func TestVAlterSubclusterTypeOptions_validateParseOptions(t *testing.T) {
	logger := vlog.Printer{}

	opt := VPromoteDemoteFactory()
	testPassword := "test-password-1"

	opt.SCName = testSCName
	opt.IsEon = true
	opt.RawHosts = append(opt.RawHosts, "test-raw-host")
	opt.DBName = testDBName
	opt.UserName = testUserName
	opt.Password = &testPassword
	opt.SCType = Primary

	err := opt.validateParseOptions(logger)
	assert.NoError(t, err)

	opt.UserName = ""
	err = opt.validateParseOptions(logger)
	assert.NoError(t, err)

	// negative: no database name
	opt.UserName = testUserName
	opt.DBName = ""
	err = opt.validateParseOptions(logger)
	assert.ErrorContains(t, err, "must specify a database name")

	// negative: no subcluster name
	opt.DBName = testDBName
	opt.SCName = ""
	err = opt.validateParseOptions(logger)
	assert.ErrorContains(t, err, "must specify a subcluster name")

	// negative: enterprise database
	opt.IsEon = false
	err = opt.validateParseOptions(logger)
	assert.ErrorContains(t, err, "promote or demote subclusters are only supported in Eon mode")
}
