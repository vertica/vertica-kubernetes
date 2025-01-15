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
	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestTimeoutErrorCase(t *testing.T) {
	var instructions []clusterOp
	// use a non-existing IP to test the timeout error
	// 192.0.2.1 is one that is reserved for test purpose (by RFC 5737)
	hosts := []string{"192.0.2.1"}
	username := "testUser"
	password := "testPwd"
	// Intentionally pick a low http request timeout to speed up the test.
	const httpRequestTimeoutForTest = 3
	httpsPollNodeStateOp, err := makeHTTPSPollNodeStateOp(hosts, true, username, &password, 0)
	assert.Nil(t, err)
	httpsPollNodeStateOp.httpRequestTimeout = httpRequestTimeoutForTest
	instructions = append(instructions, &httpsPollNodeStateOp)

	// default timeout value for the op
	clusterOpEngine := makeClusterOpEngine(instructions, nil)
	err = clusterOpEngine.run(vlog.Printer{})
	// expect timeout error in http response
	assert.ErrorContains(t, err, "[HTTPSPollNodeStateOp] cannot connect to host 192.0.2.1, please check if the host is still alive")

	// negative timeout value for the op (treated as 0, means no polling)
	instructions = make([]clusterOp, 0)
	httpsPollNodeStateOp, err = makeHTTPSPollNodeStateOp(hosts, true, username, &password, -100)
	httpsPollNodeStateOp.cmdType = CreateDBCmd
	assert.Nil(t, err)
	httpsPollNodeStateOp.httpRequestTimeout = httpRequestTimeoutForTest
	instructions = append(instructions, &httpsPollNodeStateOp)
	clusterOpEngine = makeClusterOpEngine(instructions, nil)
	err = clusterOpEngine.run(vlog.Printer{})
	// negative value means no polling timeout
	assert.ErrorContains(t, err, "[HTTPSPollNodeStateOp] cannot connect to host")
}
