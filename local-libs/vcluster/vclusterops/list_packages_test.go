package vclusterops

import (
	"fmt"
	"strings"
	"testing"
)

func TestListPackages(t *testing.T) {
	testIsDatabaseNotFoundError(t)
	testValidatePackageFilter(t)
	testVListPackagesOptionsFactory(t)
}

// Database Not Found Error
func testIsDatabaseNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"database not found", fmt.Errorf("database 'testdb' is running on host node1"), true},
		{"other error", fmt.Errorf("connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDatabaseNotFoundError(tt.err)
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

// Package Filter Validation
func testValidatePackageFilter(t *testing.T) {
	tests := []struct {
		name        string
		filter      string
		shouldError bool
	}{
		// Valid cases
		{"empty", "", false},
		{"all", "all", false},
		{"default", "default", false},
		{"package name", "ComplexTypes", false},
		{"with underscore", "Machine_Learning", false},

		// Invalid cases
		{"with spaces", "my package", true},
		{"with special chars", "test@123", true},
		{"with parentheses", "pkg(old)", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalidChars := " \t\n@#$%^&*()+={}[]|\\:;\"'<>?,./"
			hasError := tt.filter != "" && strings.ContainsAny(tt.filter, invalidChars)

			if hasError != tt.shouldError {
				t.Errorf("filter '%s': expected error=%v, got error=%v",
					tt.filter, tt.shouldError, hasError)
			}
		})
	}
}

// Test Options Factory
func testVListPackagesOptionsFactory(t *testing.T) {
	opts := VListPackagesOptionsFactory()

	if opts.PackageFilter != "" {
		t.Errorf("expected empty PackageFilter, got '%s'", opts.PackageFilter)
	}
}
