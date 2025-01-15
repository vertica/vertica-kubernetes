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

func TestRequiredOptions(t *testing.T) {
	options := VFetchNodesDetailsOptionsFactory()
	vcc := VClusterCommands{}

	// dbName is required
	nodesDetails, err := vcc.VFetchNodesDetails(&options)
	assert.Empty(t, nodesDetails)
	assert.ErrorContains(t, err, `must specify a database name`)

	// hosts are required
	options.DBName = "testDB"
	nodesDetails, err = vcc.VFetchNodesDetails(&options)
	assert.Empty(t, nodesDetails)
	assert.ErrorContains(t, err, `must specify a host or host list`)
}
