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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type mockOp struct {
	opBase
	calledPrepare  bool
	calledExecute  bool
	calledFinalize bool
}

func makeMockOp(skipExecute bool) mockOp {
	return mockOp{
		opBase: opBase{
			name:        fmt.Sprintf("skip-enabled-%v", skipExecute),
			skipExecute: skipExecute,
		},
	}
}

func (m *mockOp) prepare(_ *opEngineExecContext) error {
	m.calledPrepare = true
	if !m.skipExecute {
		return m.setupClusterHTTPRequest([]string{"host1"})
	}
	return nil
}

func (m *mockOp) execute(_ *opEngineExecContext) error {
	m.calledExecute = true
	return nil
}

func (m *mockOp) finalize(_ *opEngineExecContext) error {
	m.calledFinalize = true
	return nil
}

func (m *mockOp) processResult(_ *opEngineExecContext) error {
	return nil
}

func (m *mockOp) setupClusterHTTPRequest(hosts []string) error {
	m.clusterHTTPRequest.RequestCollection = map[string]hostHTTPRequest{}
	for i := range hosts {
		m.clusterHTTPRequest.RequestCollection[hosts[i]] = hostHTTPRequest{}
	}
	return nil
}

func TestSkipExecuteOp(t *testing.T) {
	opWithSkipEnabled := makeMockOp(true)
	opWithSkipDisabled := makeMockOp(false)
	instructions := []clusterOp{&opWithSkipDisabled, &opWithSkipEnabled}
	opEngn := makeClusterOpEngine(instructions, nil)
	err := opEngn.run(vlog.Printer{})
	assert.Equal(t, nil, err)
	assert.True(t, opWithSkipDisabled.calledPrepare)
	assert.True(t, opWithSkipDisabled.calledExecute)
	assert.True(t, opWithSkipDisabled.calledFinalize)
	assert.True(t, opWithSkipEnabled.calledPrepare)
	assert.False(t, opWithSkipEnabled.calledExecute)
	assert.True(t, opWithSkipEnabled.calledFinalize)
}
