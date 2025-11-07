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
	"fmt"
	"regexp"
	"strings"
)

// Package filter constants
const (
	PkgStatusYes     = "Yes"
	PkgStatusNo      = "No"
	PkgFilterAll     = "all"
	PkgFilterDefault = "default"
)

// Package name validation: follows Vertica unquoted identifier rule
//
// Valid examples:
//   - "ComplexTypes"
//   - "package123"
//
// Invalid examples:
//   - "123package"    (starts with digit)
//   - "package-name"  (contains hyphen)
//   - "package name"  (contains space)
var packageNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_$]{0,127}$`)

// ValidatePackageFilter validates the package filter string.
func ValidatePackageFilter(packageFilter string) error {
	if packageFilter != "" {
		filter := packageFilter

		// Allow special keywords
		if filter != PkgFilterAll && filter != PkgFilterDefault {
			// Split by comma to handle comma-separated lists
			packages := strings.Split(filter, ",")
			validPackageCount := 0
			for _, pkg := range packages {
				pkg = strings.TrimSpace(pkg)
				if pkg != "" {
					validPackageCount++
					if !packageNameRegex.MatchString(pkg) {
						return fmt.Errorf("invalid package name: %s", pkg)
					}
				}
			}
			// --package "," or --package " " or --package ", , ,", etc.
			if validPackageCount == 0 {
				return fmt.Errorf("no valid package names provided")
			}
		}
	}
	return nil
}
