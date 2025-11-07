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
)

// Package Filter Validation
func TestValidatePackageFilter(t *testing.T) {
	tests := []struct {
		name        string
		filter      string
		shouldError bool
	}{
		// Valid cases
		{"empty", "", false},
		{"all", "all", false},
		{"default", "default", false},
		{"single package", "kafka", false},
		{"comma separated", "kafka,ComplexTypes", false},
		{"empty after trim", "kafka,  ,ComplexTypes", false},

		// Invalid cases
		{"starts with number", "123invalid", true},
		{"with special char", "my-package", true},
		{"only commas", ",,,", true},
		{"only spaces", "   ", true},
		{"invalid in list", "kafka,123bad", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePackageFilter(tt.filter)
			hasError := err != nil

			if hasError != tt.shouldError {
				t.Errorf("filter '%s': expected error=%v, got error=%v (err: %v)",
					tt.filter, tt.shouldError, hasError, err)
			}
		})
	}
}
