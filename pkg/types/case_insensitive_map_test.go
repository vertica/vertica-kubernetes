/*
 (c) Copyright [2021-2023] Open Text.
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

package types

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNames(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "case_insensitive_map Suite")
}

var _ = Describe("case_insensitive_map", func() {
	It("should set a case insensitive map", func() {
		c := MakeCiMap()
		c.Set("key1", "val1")
		c.Set("kEY1", "val2")
		Expect(c.Size()).Should(Equal(1))
		Expect(c.GetValue("kEY1")).Should(Equal("val2"))
		Expect(c.ContainKeyValuePair("KEY1", "val2")).Should(Equal(true))
	})

	It("should create a map from a cimap", func() {
		c := MakeCiMap()
		c.Set("kEY2", "v2")
		c.Set("kEY3", "v3")
		m := c.GetMap()
		Expect(len(m)).Should(Equal(2))
		Expect(m["key2"]).Should(Equal("v2"))
	})

	It("should check if a key does not exist", func() {
		c := MakeCiMap()
		c.Set("kEY4", "v4")
		Expect(c.GetValue("kEY5")).Should(Equal(""))
		// Right value, wrong key
		Expect(c.ContainKeyValuePair("KEY5", "v4")).Should(Equal(false))
		// Right key, wrong value
		Expect(c.ContainKeyValuePair("KEY4", "v5")).Should(Equal(false))
	})
})
