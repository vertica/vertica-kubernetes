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

package vlog

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// CaptureStdout returns the stdout of the function f as a string
func CaptureStdout(f func()) string {
	originalStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = originalStdout

	return string(out)
}

func TestPasswordRedaction(t *testing.T) {
	// test pw redaction
	pw := "hunter2"
	pwArgv := []string{"--password", pw}
	maskedArgs := logMaskedArgParseHelper(pwArgv)
	assert.Len(t, maskedArgs, 2)
	assert.NotEqual(t, pw, maskedArgs[1])

	// test non-sensitive is not redacted
	argv := []string{"--nothing-secret", pw}
	unmaskedArgs := logMaskedArgParseHelper(argv)
	assert.Len(t, unmaskedArgs, 2)
	assert.Equal(t, pw, unmaskedArgs[1])
}
func TestLogMaskedArgParseHelper(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "Mask password argument",
			input:    []string{"--password", "mypassword"},
			expected: []string{"--password", "******"},
		},
		{
			name:     "Mask short password argument",
			input:    []string{"-p", "mypassword"},
			expected: []string{"-p", "******"},
		},
		{
			name:     "Mask config-param with sensitive keys",
			input:    []string{"--config-param", "awsauth=key1,gcsauth=key2"},
			expected: []string{"--config-param", "awsauth=******", "--config-param", "gcsauth=******"},
		},
		{
			name:     "Handle non-sensitive config-param keys",
			input:    []string{"--config-param", "nonsensitive=value"},
			expected: []string{"--config-param", "nonsensitive=value"},
		},
		{
			name:     "Handle invalid config-param format",
			input:    []string{"--config-param", "invalidformat"},
			expected: []string{"--config-param", "invalidformat"},
		},
		{
			name:     "Non-sensitive argument remains unmasked",
			input:    []string{"--nothing-secret", "value"},
			expected: []string{"--nothing-secret", "value"},
		},
		{
			name:     "Mixed arguments with masking",
			input:    []string{"--password", "mypassword", "--config-param", "awsauth=key1,nonsensitive=value"},
			expected: []string{"--password", "******", "--config-param", "awsauth=******", "--config-param", "nonsensitive=value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := logMaskedArgParseHelper(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
