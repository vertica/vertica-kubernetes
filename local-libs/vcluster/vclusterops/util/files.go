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
	"encoding/csv"
	"fmt"
	"os"
	"reflect"
	"strconv"
)

const csvTag = "csv"

// WriteCSV will create and write to a CSV file.
// 'rows' should be a 2D slice where each inner slice is a row, and each element in the inner slice is a column
// The first row should contain headers (if using)
func WriteCSV(path string, rows [][]string, permissions os.FileMode) error {
	// Create a new CSV file
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("fail to create file at path '%s': %w", path, err)
	}
	defer file.Close()

	// Change file permissions
	if err := os.Chmod(path, permissions); err != nil {
		return fmt.Errorf("fail to set file permissions: %w", err)
	}

	// Create a CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write CSV data to the file
	for _, row := range rows {
		err := writer.Write(row)
		if err != nil {
			return fmt.Errorf("fail to write CSV record: %w", err)
		}
	}

	return nil
}

// ReadCSV will parse a CSV file.
// Results are returned as a 2D slice where each inner slice is a row, and each element in the inner slice is a column
// The first row will contain headers (if the file contains them)
func ReadCSV(path string) ([][]string, error) {
	// Open the CSV file
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fail to open file at path '%s': %w", path, err)
	}
	defer file.Close()

	// Create a CSV reader
	reader := csv.NewReader(file)

	// Read CSV data from the file
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("fail to read the CSV file: %v", err)
	}

	return records, nil
}

// Convert a slice of structs to a 2D slice of strings (the format expected by WriteCSV)
func ConvertToCSVRows(slice any) ([][]string, error) {
	// Ensure the input is a slice
	sliceType := reflect.TypeOf(slice)
	if sliceType.Kind() != reflect.Slice {
		return nil, fmt.Errorf("expected a slice")
	}

	// Ensure the elements in the input slice are structs
	elementType := sliceType.Elem()
	if elementType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected slice to contain structs")
	}

	var rows [][]string

	// Extract headers from the element type
	headers := getCSVHeaders(elementType)
	rows = append(rows, headers)

	// Construct rows by looping through each element in the slice
	sliceValue := reflect.ValueOf(slice)
	for i := 0; i < sliceValue.Len(); i++ {
		elementValue := sliceValue.Index(i)
		row := getCSVRowValues(elementValue)
		rows = append(rows, row)
	}

	return rows, nil
}

// Get CSV headers based on the element field names or 'csv' tag if present
func getCSVHeaders(elementType reflect.Type) []string {
	var headers []string
	for i := 0; i < elementType.NumField(); i++ {
		csvTag := elementType.Field(i).Tag.Get(csvTag)
		if csvTag != "" {
			headers = append(headers, csvTag)
		} else {
			headers = append(headers, elementType.Field(i).Name)
		}
	}
	return headers
}

// Get CSV row values from an element
func getCSVRowValues(elementValue reflect.Value) []string {
	var row []string
	for i := 0; i < elementValue.NumField(); i++ {
		fieldValue := elementValue.Field(i)

		// Handle different types by converting to string
		var fieldValueStr string
		switch fieldValue.Kind() {
		case reflect.String:
			fieldValueStr = fieldValue.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fieldValueStr = strconv.FormatInt(fieldValue.Int(), 10)
		case reflect.Float32, reflect.Float64:
			fieldValueStr = strconv.FormatFloat(fieldValue.Float(), 'f', -1, 64)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			fieldValueStr = strconv.FormatUint(fieldValue.Uint(), 10)
		case reflect.Bool:
			fieldValueStr = strconv.FormatBool(fieldValue.Bool())
		case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Array, reflect.Chan,
			reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.Struct,
			reflect.UnsafePointer:
			fieldValueStr = fmt.Sprintf("%v", fieldValue.Interface()) // Fallback for unknown types
		}

		row = append(row, fieldValueStr)
	}

	return row
}

// Convert a 2D slice of strings (the format returned by ReadCSV) to a slice of structs
func ConvertFromCSVRows[T any](rows [][]string) ([]T, error) {
	if reflect.ValueOf(new(T)).Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct type")
	}

	// Empty CSV - nothing to do
	if len(rows) == 0 {
		return []T{}, nil
	}

	// Build a map of CSV tag to field index for faster lookups later
	fieldIndexMap := make(map[string]int)
	outputType := reflect.TypeOf(new(T)).Elem()
	for i := 0; i < outputType.NumField(); i++ {
		field := outputType.Field(i)
		tag := field.Tag.Get(csvTag)
		if tag != "" {
			fieldIndexMap[tag] = i
		} else {
			fieldIndexMap[field.Name] = i
		}
	}

	// Get the headers from the first row
	headers := rows[0]

	outputRows := []T{}
	for rowIndex, row := range rows[1:] {
		// Create a new instance of the struct type
		outputRow := new(T)
		outputValue := reflect.ValueOf(outputRow)

		// Loop through the CSV headers and set the corresponding struct fields
		for colIndex, header := range headers {
			// Look up the field index
			fieldIndex, found := fieldIndexMap[header]
			if !found {
				return nil, fmt.Errorf("invalid header '%s' does not match any field", header)
			}

			// Get the field and ensure we can set its value
			fieldValue := outputValue.Elem().Field(fieldIndex)
			if !fieldValue.IsValid() || !fieldValue.CanSet() {
				return nil, fmt.Errorf("can't set field '%s'", fieldValue.Type().Name())
			}

			// Set the field
			err := setFieldValue(row[colIndex], fieldValue)
			if err != nil {
				return nil, fmt.Errorf("in row %d: %v", rowIndex+1, err)
			}
		}

		// Append the item to the result slice
		outputRows = append(outputRows, *outputRow)
	}

	return outputRows, nil
}

// Set a field's value by converting it from a string to the correct type
func setFieldValue(stringValue string, fieldValue reflect.Value) error {
	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(stringValue)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		intValue, err := strconv.ParseInt(stringValue, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse integer: %v", err)
		}
		fieldValue.SetInt(intValue)

	case reflect.Float32, reflect.Float64:
		floatValue, err := strconv.ParseFloat(stringValue, 64)
		if err != nil {
			return fmt.Errorf("failed to parse float: %v", err)
		}
		fieldValue.SetFloat(floatValue)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintValue, err := strconv.ParseUint(stringValue, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse unsigned integer: %v", err)
		}
		fieldValue.SetUint(uintValue)

	case reflect.Bool:
		boolValue, err := strconv.ParseBool(stringValue)
		if err != nil {
			return fmt.Errorf("failed to parse boolean: %v", err)
		}
		fieldValue.SetBool(boolValue)

	case reflect.Invalid, reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Array, reflect.Chan,
		reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.Struct,
		reflect.UnsafePointer:
		return fmt.Errorf("cannot convert string to type '%s'", fieldValue.Kind().String())
	}

	return nil
}
