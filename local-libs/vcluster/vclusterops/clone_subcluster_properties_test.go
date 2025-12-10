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
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// Test constants
const (
	testClonePropDBName = "testdb"
	testClonePropHost   = "host1"
	testClonePropSC1    = "sc1"
	testClonePropSC2    = "sc2"
)

// master function
func TestCloneSubclusterPropertiesRequiredOptions(t *testing.T) {
	testCloneSubclusterPropertiesRequiredOptionsSameSourceAndTarget(t)
	testCloneSubclusterPropertiesRequiredOptionsEmptySource(t)
	testCloneSubclusterPropertiesRequiredOptionsEmptyTarget(t)
}

func testCloneSubclusterPropertiesRequiredOptionsSameSourceAndTarget(t *testing.T) {
	options := VCloneSubclusterPropertiesOptionsFactory()
	options.DBName = testClonePropDBName
	options.RawHosts = []string{testClonePropHost}
	options.SourceSubcluster = testClonePropSC1
	options.TargetSubcluster = testClonePropSC1

	logger := vlog.Printer{}
	err := options.validateRequiredOptions(logger)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "source and target subclusters cannot be the same")
}

func testCloneSubclusterPropertiesRequiredOptionsEmptySource(t *testing.T) {
	options := VCloneSubclusterPropertiesOptionsFactory()
	options.DBName = testClonePropDBName
	options.RawHosts = []string{testClonePropHost}
	options.SourceSubcluster = ""
	options.TargetSubcluster = testClonePropSC2

	logger := vlog.Printer{}
	err := options.validateRequiredOptions(logger)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify a source subcluster name")
}

func testCloneSubclusterPropertiesRequiredOptionsEmptyTarget(t *testing.T) {
	options := VCloneSubclusterPropertiesOptionsFactory()
	options.DBName = testClonePropDBName
	options.RawHosts = []string{testClonePropHost}
	options.SourceSubcluster = testClonePropSC1
	options.TargetSubcluster = ""

	logger := vlog.Printer{}
	err := options.validateRequiredOptions(logger)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify a target subcluster name")
}
