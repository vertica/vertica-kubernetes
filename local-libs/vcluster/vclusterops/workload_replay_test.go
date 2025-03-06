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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	fileExists        = "/exists.csv"
	fileDoesntExist   = "/doesnt-exist.csv"
	nonCSVFile        = "/not-csv.txt"
	pathTraversalFile = "../../results.csv"
)

func createTestFile(t *testing.T, path string) {
	file, err := os.Create(path)
	assert.NoError(t, err)
	defer file.Close()
}

func chmodTestFile(t *testing.T, path string, permissions os.FileMode) {
	err := os.Chmod(path, permissions)
	assert.NoError(t, err)
}

func TestValidateWorkloadFileLocation(t *testing.T) {
	options := VWorkloadReplayOptionsFactory()

	// Create a file that can be read
	tempDir := t.TempDir()
	testFile := tempDir + fileExists
	createTestFile(t, testFile)
	chmodTestFile(t, testFile, os.FileMode(0600))

	// Positive - file exists and can be read
	options.WorkloadFileLocation = testFile
	err := options.validateWorkloadFileLocation()
	assert.NoError(t, err)

	// Negative - empty path
	options.WorkloadFileLocation = ""
	err = options.validateWorkloadFileLocation()
	assert.ErrorContains(t, err, "must provide workload file location")

	// Negative - non-CSV file provided
	options.WorkloadFileLocation = tempDir + nonCSVFile
	err = options.validateWorkloadFileLocation()
	assert.ErrorContains(t, err, "must provide .csv workload file")

	// Negative - non-absolute path
	options.WorkloadFileLocation = pathTraversalFile
	err = options.validateWorkloadFileLocation()
	assert.ErrorContains(t, err, "must provide an absolute path for workload file location")

	// Negative - file doesn't exist
	options.WorkloadFileLocation = tempDir + fileDoesntExist
	err = options.validateWorkloadFileLocation()
	assert.ErrorContains(t, err, "workload file location does not exist")

	// Negative - file exists but can't be read
	chmodTestFile(t, testFile, os.FileMode(0300))
	options.WorkloadFileLocation = testFile
	err = options.validateWorkloadFileLocation()
	assert.ErrorContains(t, err, "no permission to read from workload file location")
}

func TestValidateReplayResultsFileLocation(t *testing.T) {
	options := VWorkloadReplayOptionsFactory()

	// Positive - valid path
	options.ReplayResultsFileLocation = t.TempDir() + "/results.csv"
	err := options.validateReplayResultsFileLocation()
	assert.NoError(t, err)

	// Negative - empty path
	options.ReplayResultsFileLocation = ""
	err = options.validateReplayResultsFileLocation()
	assert.ErrorContains(t, err, "must provide replay results file location")

	// Negative - non-absolute path
	options.ReplayResultsFileLocation = pathTraversalFile
	err = options.validateReplayResultsFileLocation()
	assert.ErrorContains(t, err, "must provide an absolute path for replay results file location")

	// Create test file
	tempDir := t.TempDir()
	testFile := tempDir + fileExists
	createTestFile(t, testFile)

	// Negative - file already exists
	options.ReplayResultsFileLocation = testFile
	err = options.validateReplayResultsFileLocation()
	assert.ErrorContains(t, err, "file already exists at replay results file location")
}

func TestValidateSandbox(t *testing.T) {
	options := VWorkloadReplayOptionsFactory()

	// Positive - Valid sandbox name
	options.Sandbox = "sandbox1"
	err := options.validateSandbox()
	assert.NoError(t, err)

	// Negative - empty string
	options.Sandbox = ""
	err = options.validateSandbox()
	assert.ErrorContains(t, err, "must provide sandbox")

	// Negative - invalid characters
	options.Sandbox = "$@ndbox"
	err = options.validateSandbox()
	assert.ErrorContains(t, err, "invalid character in sandbox name")
}

func TestAggregateWorkloadReplayReportData(t *testing.T) {
	const (
		query1 = "select * from table1;"
		query2 = "select * from table2;"
		node1  = "node1"
		node2  = "node2"
	)

	originalData := []workloadQuery{
		{
			NodeName:          node1,
			Request:           query1,
			RequestDurationMS: 10,
		},
		{
			NodeName:          node1,
			Request:           query2,
			RequestDurationMS: 20,
		},
	}
	replayData := []workloadQuery{
		{
			NodeName:          node2,
			Request:           query1,
			RequestDurationMS: 100,
		},
		{
			NodeName:          node2,
			Request:           query2,
			RequestDurationMS: 200,
			ErrorDetails:      "error running query",
		},
	}
	input := workloadReplayData{
		originalWorkloadData: originalData,
		replayData:           replayData,
	}

	expected := []workloadReplayReportData{
		{
			Request:            query1,
			OriginalDurationMS: 10,
			OriginalNodeName:   node1,
			ReplayDurationMS:   100,
			ReplayNodeName:     node2,
			ErrorDetails:       "",
		},
		{
			Request:            query2,
			OriginalDurationMS: 20,
			OriginalNodeName:   node1,
			ReplayDurationMS:   200,
			ReplayNodeName:     node2,
			ErrorDetails:       "error running query",
		},
	}

	actual := aggregateWorkloadReplayReportData(input)
	assert.Equal(t, expected, actual)
}
