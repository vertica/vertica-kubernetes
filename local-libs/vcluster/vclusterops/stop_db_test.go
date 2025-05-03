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

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestVStopDatabaseOptions_validateParseOptions(t *testing.T) {
	logger := vlog.Printer{}

	opt := VStopDatabaseOptionsFactory()
	testPd := "stop-db-test-password"
	testSandbox := "stop-db-test-sandbox"
	testDBName := "stop_db_test_dbname"
	testUsername := "stop-db-test-un"
	testHost := "stop-db-test-host"

	opt.SandboxName = testSandbox
	opt.RawHosts = append(opt.RawHosts, testHost)
	opt.DBName = testDBName
	opt.UserName = testUsername
	opt.Password = &testPd
	opt.IsEon = true // test Eon behavior
	opt.DrainSeconds = nil

	// positive: valid options
	err := opt.validateParseOptions(logger)
	assert.NoError(t, err)
	assert.NotNil(t, opt.DrainSeconds)
	assert.Equal(t, util.DefaultDrainSeconds, *opt.DrainSeconds)

	// positive: enterprise db (non-eon), drain seconds should be ignored
	opt.IsEon = false
	seconds := 30
	opt.DrainSeconds = &seconds
	err = opt.validateParseOptions(logger)
	assert.NoError(t, err)
	assert.Nil(t, opt.DrainSeconds)

	// positive: MainCluster is true and SandboxName is empty
	opt.IsEon = true
	opt.DrainSeconds = nil
	opt.MainCluster = true
	opt.SandboxName = ""
	err = opt.validateParseOptions(logger)
	assert.NoError(t, err)

	// negative: both MainCluster and SandboxName set
	opt.MainCluster = true
	opt.SandboxName = testSandbox
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)

	// negative: invalid sandbox name
	opt.MainCluster = false
	opt.SandboxName = "Invalid Sandbox!"
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)

	// negative: missing DBName
	opt.SandboxName = testSandbox
	opt.DBName = ""
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)

	// negative: missing raw hosts
	opt.DBName = testDBName
	opt.RawHosts = []string{}
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)
}

func TestVStopDatabaseOptions_checkStopDBRequirements(t *testing.T) {
	opt := VStopDatabaseOptionsFactory()
	const sbName = "sandbox-a"
	vdb := VCoordinationDatabase{
		HostNodeMap: map[string]*VCoordinationNode{
			"10.0.0.1": {Sandbox: ""},
			"10.0.0.2": {Sandbox: sbName},
		},
	}

	// Positive Case 1: main cluster host is in list
	opt.SandboxName = ""    // no sandbox specified
	opt.MainCluster = false // not explicitly asking for main cluster
	opt.Hosts = []string{"10.0.0.1"}
	err := opt.checkStopDBRequirements(&vdb)
	assert.NoError(t, err)

	// Negative Case 2: no main cluster host in list
	opt.Hosts = []string{"10.0.0.2"}
	err = opt.checkStopDBRequirements(&vdb)
	assert.Error(t, err)

	// Positive Case 3: sandbox or main cluster explicitly specified
	opt.SandboxName = sbName
	err = opt.checkStopDBRequirements(&vdb)
	assert.NoError(t, err)

	opt.SandboxName = ""
	opt.MainCluster = true
	err = opt.checkStopDBRequirements(&vdb)
	assert.NoError(t, err)
}

func TestVStopDatabaseOptions_setAllHosts(t *testing.T) {
	opt := VStopDatabaseOptionsFactory()
	const sba = "sandbox-a"
	vdb := VCoordinationDatabase{
		HostNodeMap: map[string]*VCoordinationNode{
			"10.0.0.1": {Sandbox: ""},
			"10.0.0.2": {Sandbox: sba},
			"10.0.0.3": {Sandbox: "sandbox-b"},
			"10.0.0.4": {Sandbox: ""},
		},
	}

	// Positive Case 1: collect only main cluster hosts
	opt.MainCluster = true
	opt.setAllHosts(&vdb)
	assert.ElementsMatch(t, []string{"10.0.0.1", "10.0.0.4"}, opt.Hosts)

	// Positive Case 2: collect only sandbox-a hosts
	opt.MainCluster = false
	opt.SandboxName = sba
	opt.setAllHosts(&vdb)
	assert.ElementsMatch(t, []string{"10.0.0.2"}, opt.Hosts)

	// Positive Case 3: collect all cluster hosts using sandbox name
	opt.SandboxName = util.MainClusterSandbox
	opt.setAllHosts(&vdb)
	assert.ElementsMatch(t, []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}, opt.Hosts)
}
