/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package vdbgen

import (
	"bytes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

type FakeGenerator struct {
	vdb *vapi.VerticaDB
}

func (f *FakeGenerator) Create() (*KObjs, error) {
	return &KObjs{Vdb: *f.vdb}, nil
}

var _ = Describe("generator", func() {
	It("should generate yaml from mock VDB", func() {
		vdb := vapi.MakeVDB()
		buf := &bytes.Buffer{}
		Expect(Generate(buf, &FakeGenerator{vdb: vdb})).Should(Succeed())
		Expect(buf.String()).Should(ContainSubstring("shardCount: %d", vdb.Spec.ShardCount))
		Expect(buf.String()).Should(ContainSubstring("dbName: %s", vdb.Spec.DBName))
	})
})
