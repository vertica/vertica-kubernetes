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
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestReIPOptions(t *testing.T) {
	opt := VReIPFactory()
	err := opt.validateAnalyzeOptions(vlog.Printer{})
	assert.Error(t, err)

	opt.DBName = "test_db"
	opt.RawHosts = []string{"192.168.1.101", "192.168.1.102"}
	err = opt.validateAnalyzeOptions(vlog.Printer{})
	assert.ErrorContains(t, err, "must specify an absolute catalog path")

	opt.CatalogPrefix = "/data"
	err = opt.validateAnalyzeOptions(vlog.Printer{})
	assert.ErrorContains(t, err, "the re-ip list is not provided")

	var info ReIPInfo
	info.NodeAddress = "192.168.1.102"
	info.TargetAddress = "192.168.1.103"
	opt.ReIPList = append(opt.ReIPList, info)
	err = opt.validateAnalyzeOptions(vlog.Printer{})
	assert.NoError(t, err)
}

func TestReadReIPFile(t *testing.T) {
	opt := VReIPFactory()
	currentDir, _ := os.Getwd()

	// ipv4 positive
	opt.IPv6 = false
	err := opt.ReadReIPFile(currentDir + "/test_data/re_ip_v4.json")
	assert.NoError(t, err)

	// ipv4 negative
	err = opt.ReadReIPFile(currentDir + "/test_data/re_ip_v4_wrong.json")
	assert.ErrorContains(t, err, "192.168.1.10a in the re-ip file is not a valid IPv4 address")

	// ipv6
	opt.IPv6 = true
	err = opt.ReadReIPFile(currentDir + "/test_data/re_ip_v6.json")
	assert.NoError(t, err)

	// ipv6 negative
	err = opt.ReadReIPFile(currentDir + "/test_data/re_ip_v6_wrong.json")
	assert.ErrorContains(t, err, "0:0:0:0:0:ffff:c0a8:016-6 in the re-ip file is not a valid IPv6 address")
}

func TestTrimReIPList(t *testing.T) {
	// build a stub exec context
	log := vlog.Printer{}
	var op nmaReIPOp
	execContext := makeOpEngineExecContext(log)

	// build a stub NmaVDatabase
	nmaVDB := nmaVDatabase{}
	for i := 0; i < 3; i++ {
		vnode := nmaVNode{}
		vnode.Address = fmt.Sprintf("vnode%d", i+1)
		vnode.Name = fmt.Sprintf("v_%s_node000%d", dbName, i+1)
		nmaVDB.Nodes = append(nmaVDB.Nodes, vnode)
	}
	execContext.nmaVDatabase = nmaVDB

	// build a stub re-ip list
	// which has an extra node compared to the actual NmaVDatabase
	for i := 0; i < 4; i++ {
		var info ReIPInfo
		info.NodeName = fmt.Sprintf("v_%s_node000%d", dbName, i+1)
		info.NodeAddress = fmt.Sprintf("vnode%d", i+1)
		info.TargetAddress = fmt.Sprintf("vnode_new_%d", i+1)
		op.reIPList = append(op.reIPList, info)
	}

	// re-ip list before trimming
	assert.Equal(t, len(op.reIPList), 4)

	err := op.trimReIPList(&execContext)
	assert.ErrorContains(t, err,
		"the following nodes from the re-ip list do not exist in the catalog")

	// re-ip list after trimming: the extra node is trimmed off
	op.trimReIPData = true
	err = op.trimReIPList(&execContext)
	assert.NoError(t, err)
	assert.Equal(t, len(op.reIPList), 3)
}
