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
	Name       string  `csv:"name"`
	Age        int     `csv:"age"`
	PreciseAge float64 `csv:"precise_age"`
	IsAlive    bool    `csv:"is_alive"`
}

// Test data as a slice of structs
func getTestPeople() []TestPerson {
	return []TestPerson{
		{
			Name:       "John Smith",
			Age:        25,
			PreciseAge: 25.456,
			IsAlive:    true,
		},
		{
			Name:       "Jane Doe",
			Age:        73,
			PreciseAge: 73.333,
			IsAlive:    false,
		},
	}
}

// The same test data as a 2D slice of strings. We should be able to convert between the two representations
func getTestPeopleCSVRows() [][]string {
	return [][]string{
		{"name", "age", "precise_age", "is_alive"},
		{"John Smith", "25", "25.456", "true"},
		{"Jane Doe", "73", "73.333", "false"},
	}
}

func TestConvertToCSVRows(t *testing.T) {
	testPeople := getTestPeople()
	expectedRows := getTestPeopleCSVRows()

	// Positive
	actualRows, err := ConvertToCSVRows(testPeople)
	assert.NoError(t, err)
	assert.Equal(t, expectedRows, actualRows)

	// Negative - input string
	inputStr := "input"
	_, err = ConvertToCSVRows(inputStr)
	assert.ErrorContains(t, err, "expected a slice")

	// Negative - input slice of strings
	inputSlice := []string{"str1", "str2"}
	_, err = ConvertToCSVRows(inputSlice)
	assert.ErrorContains(t, err, "expected slice to contain structs")
}

func TestConvertFromCSVRows(t *testing.T) {
	// Positive
	inputRows := getTestPeopleCSVRows()
	actualPeople, err := ConvertFromCSVRows[TestPerson](inputRows)
	expectedPeople := getTestPeople()
	assert.NoError(t, err)
	assert.Equal(t, expectedPeople, actualPeople)

	// Positive - empty input slice
	inputRows = [][]string{{}}
	actualPeople, err = ConvertFromCSVRows[TestPerson](inputRows)
	expectedPeople = []TestPerson{}
	assert.NoError(t, err)
	assert.Equal(t, expectedPeople, actualPeople)

	// Negative - headers don't match TestPerson struct
	inputRows = [][]string{
		{"invalid_header_name"},
		{"24"},
	}
	_, err = ConvertFromCSVRows[TestPerson](inputRows)
	assert.ErrorContains(t, err, "invalid header 'invalid_header_name' does not match any field")

	// Negative - output structs should be flat (no slices, nested structs, etc.)
	type InvalidTestType struct {
		Slice []string
	}
	inputRows = [][]string{
		{"Slice"},
		{"[1,2,3,4]"},
	}
	_, err = ConvertFromCSVRows[InvalidTestType](inputRows)
	assert.ErrorContains(t, err, "cannot convert string to type 'slice'")

	// Negative - parameterized type should be a struct
	inputRows = [][]string{}
	_, err = ConvertFromCSVRows[string](inputRows)
	assert.ErrorContains(t, err, "expected a struct type")

	// Negative - invalid integer value
	inputRows = getTestPeopleCSVRows()
	inputRows = append(inputRows, []string{"John Doe", "invalid int", "123.45", "true"})
	_, err = ConvertFromCSVRows[TestPerson](inputRows)
	assert.ErrorContains(t, err, "in row 3: failed to parse integer")

	// Negative - invalid float value
	inputRows = getTestPeopleCSVRows()
	inputRows = append(inputRows, []string{"John Doe", "123", "invalid float", "true"})
	_, err = ConvertFromCSVRows[TestPerson](inputRows)
	assert.ErrorContains(t, err, "in row 3: failed to parse float")

	// Negative - invalid boolean value
	inputRows = getTestPeopleCSVRows()
	inputRows = append(inputRows, []string{"John Doe", "123", "123.45", "invalid bool"})
	_, err = ConvertFromCSVRows[TestPerson](inputRows)
	assert.ErrorContains(t, err, "in row 3: failed to parse boolean")
}
