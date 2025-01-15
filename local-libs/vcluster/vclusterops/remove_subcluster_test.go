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

const dbName = "test_db"

func TestRemoveSubcluster(t *testing.T) {
	options := VRemoveScOptionsFactory()
	options.RawHosts = []string{"vnode1", "vnode2"}
	options.Password = new(string)
	// input db name
	options.DBName = dbName

	// options without sc name, data path, and depot path
	err := options.validateParseOptions(vlog.Printer{})
	assert.ErrorContains(t, err, "must specify a subcluster name")

	// input sc name
	options.SCName = "sc1"

	// verify Eon mode is set
	options.IsEon = false
	err = options.validateParseOptions(vlog.Printer{})
	assert.ErrorContains(t, err, "cannot remove subcluster from an enterprise database")
	options.IsEon = true

	err = options.validateParseOptions(vlog.Printer{})
	assert.ErrorContains(t, err, "must specify an absolute data path")

	// input data path
	options.DataPrefix = defaultPath
	err = options.validateParseOptions(vlog.Printer{})
	assert.ErrorContains(t, err, "must specify an absolute depot path")

	// input depot path
	options.DepotPrefix = defaultPath
	err = options.validateParseOptions(vlog.Printer{})
	assert.NoError(t, err)
}
