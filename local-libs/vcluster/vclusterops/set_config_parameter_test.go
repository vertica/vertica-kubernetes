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

func TestVSetConfigurationParameterOptions_validateParseOptions(t *testing.T) {
	logger := vlog.Printer{}

	opt := VSetConfigurationParameterOptionsFactory()
	testPd := "set-config-test-pd"
	testSandbox := "set-config-test-sandbox"
	testDBName := "set_config_test_dbname"
	testUsername := "set-config-test-un"
	testConfigParameter := "set-config-test-parameter"
	testValue := "set-config-test-value"
	testLevel := "set-config-test-level"

	opt.Sandbox = testSandbox
	opt.RawHosts = append(opt.RawHosts, "set-config-test-raw-host")
	opt.DBName = testDBName
	opt.UserName = testUsername
	opt.Password = &testPd
	opt.ConfigParameter = testConfigParameter
	opt.Value = testValue
	opt.Level = testLevel

	err := opt.validateParseOptions(logger)
	assert.NoError(t, err)

	// positive: no username (in which case default OS username will be used)
	opt.UserName = ""
	err = opt.validateParseOptions(logger)
	assert.NoError(t, err)

	// positive: value is "null"
	opt.UserName = testUsername
	opt.Value = "null"
	err = opt.validateParseOptions(logger)
	assert.NoError(t, err)

	// positive: value is empty
	opt.Value = ""
	err = opt.validateParseOptions(logger)
	assert.NoError(t, err)

	// positive: empty level
	opt.Value = testValue
	opt.Level = ""
	err = opt.validateParseOptions(logger)
	assert.NoError(t, err)

	// negative: no database name
	opt.Level = testLevel
	opt.DBName = ""
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)

	// negative: no configuration parameter
	opt.DBName = testDBName
	opt.ConfigParameter = ""
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)
}
