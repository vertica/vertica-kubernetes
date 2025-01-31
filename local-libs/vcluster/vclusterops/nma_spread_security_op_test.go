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
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestSpreadOpRequestBody(t *testing.T) {
	op := makeNMASpreadSecurityOp(vlog.Printer{}, spreadKeyTypeVertica)
	const hostname = "host1"
	op.hosts = []string{hostname}
	op.catalogPathMap = map[string]string{
		hostname: "/catalog/v_db_node0001_catalog/Catalog",
	}
	requestBody, err := op.setupRequestBody()
	assert.NoError(t, err)
	assert.Len(t, requestBody, 1)
	assert.Contains(t, requestBody, hostname)
	hostReq := requestBody[hostname]
	assert.Contains(t, hostReq, `"/catalog/v_db_node0001_catalog"`)
	assert.Regexp(t, regexp.MustCompile(`"spread_security_details":.*\{.*\w+.*:.*\w+.*\}`), hostReq)
}

func TestSpreadKeyGeneration(t *testing.T) {
	op := makeNMASpreadSecurityOp(vlog.Printer{}, spreadKeyTypeVertica)
	keyID, err := op.generateKeyID()
	assert.NoError(t, err)
	const expectedKeyIDSize = 4
	assert.Equal(t, expectedKeyIDSize, len(keyID), "keyID '%s' is not %d in length", keyID, expectedKeyIDSize)
	spreadKey, err := op.generateVerticaSpreadKey()
	assert.NoError(t, err)
	const expectedSpreadKeySize = 32 * 2 // 32-bytes at 2 chars each
	assert.Equal(t, expectedSpreadKeySize, len(spreadKey), "spreadKey '%s' is not %d in length", spreadKey, expectedSpreadKeySize)
}
