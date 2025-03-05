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

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type TestPerson struct {
	Name    string `csv:"name"`
	Age     int    `csv:"age"`
	IsAlive bool   `csv:"is_alive"`
}

func TestConvertToCSVRows(t *testing.T) {
	testPeople := []TestPerson{
		{
			Name:    "John Smith",
			Age:     25,
			IsAlive: true,
		},
		{
			Name:    "Jane Doe",
			Age:     73,
			IsAlive: false,
		},
	}
	expectedRows := [][]string{
		{"name", "age", "is_alive"},
		{"John Smith", "25", "true"},
		{"Jane Doe", "73", "false"},
	}

	// Positive
	actualRows, err := ConvertToCSVRows(testPeople)
	assert.NoError(t, err)
	assert.Equal(t, expectedRows, actualRows)

	// Negative - input slice of strings
	inputStr := "input"
	_, err = ConvertToCSVRows(inputStr)
	assert.ErrorContains(t, err, "expected a slice")

	// Negative - input slice of strings
	inputSlice := []string{"str1", "str2"}
	_, err = ConvertToCSVRows(inputSlice)
	assert.ErrorContains(t, err, "expected slice to contain structs")
}
