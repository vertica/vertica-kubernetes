/*
 (c) Copyright [2023-2024] Open Text.
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
)

func TestHasQuorum(t *testing.T) {
	op := opBase{name: "test_op"}

	// positive case 1:
	hostCount := uint(2)
	primaryNodeCount := uint(3)
	succeed := op.hasQuorum(hostCount, primaryNodeCount)
	assert.Equal(t, succeed, true)

	// positive case 2:
	hostCount = 3
	primaryNodeCount = 5
	succeed = op.hasQuorum(hostCount, primaryNodeCount)
	assert.Equal(t, succeed, true)

	// positive case 3:
	hostCount = 1
	primaryNodeCount = 1
	succeed = op.hasQuorum(hostCount, primaryNodeCount)
	assert.Equal(t, succeed, true)

	// negative case 1:
	hostCount = 2
	primaryNodeCount = 6
	succeed = op.hasQuorum(hostCount, primaryNodeCount)
	assert.Equal(t, succeed, false)

	// negative case 2:
	hostCount = 2
	primaryNodeCount = 5
	succeed = op.hasQuorum(hostCount, primaryNodeCount)
	assert.Equal(t, succeed, false)

	// negative case 3:
	hostCount = 2
	primaryNodeCount = 4
	succeed = op.hasQuorum(hostCount, primaryNodeCount)
	assert.Equal(t, succeed, false)
}
