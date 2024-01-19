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

package vdb

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/types"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
)

var _ = Describe("config", func() {
	It("should have correct value of EncryptSpreadComm", func() {
		vdb := vapi.MakeVDB()

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			PRunner: fpr,
			ConfigParamsGenerator: config.ConfigParamsGenerator{
				VRec:                vdbRec,
				Log:                 logger,
				Vdb:                 vdb,
				ConfigurationParams: types.MakeCiMap(),
			},
		}

		// set the version larger than the version that will encrypt spread channel without a db restart
		vdb.Annotations[vmeta.VersionAnnotation] = "v23.3.1"
		Expect(g.Setup()).Should(Succeed())
		// default value should be "vertica"
		g.SetEncryptSpreadCommConfigIfNecessary()
		Expect(g.ConfigurationParams.Size()).Should(Equal(1))
		v, ok := g.ConfigurationParams.Get("EncryptSpreadComm")
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(vapi.EncryptSpreadCommWithVertica))
		g.ConfigurationParams = types.MakeCiMap()
		// empty string should be the same as "vertica"
		g.Vdb.Spec.EncryptSpreadComm = ""
		g.SetEncryptSpreadCommConfigIfNecessary()
		Expect(g.ConfigurationParams.Size()).Should(Equal(1))
		v, ok = g.ConfigurationParams.Get("EncryptSpreadComm")
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(vapi.EncryptSpreadCommWithVertica))
		g.ConfigurationParams = types.MakeCiMap()
		// change the value to "vertica"
		g.Vdb.Spec.EncryptSpreadComm = vapi.EncryptSpreadCommWithVertica
		g.SetEncryptSpreadCommConfigIfNecessary()
		Expect(g.ConfigurationParams.Size()).Should(Equal(1))
		v, ok = g.ConfigurationParams.Get("EncryptSpreadComm")
		Expect(ok).Should(BeTrue())
		Expect(v).Should(Equal(vapi.EncryptSpreadCommWithVertica))
		g.ConfigurationParams = types.MakeCiMap()
		// we will not encrypt spread channel when the value is "disabled"
		g.Vdb.Spec.EncryptSpreadComm = vapi.EncryptSpreadCommDisabled
		g.SetEncryptSpreadCommConfigIfNecessary()
		Expect(g.ConfigurationParams.Size()).Should(Equal(0))

		// We will not set EncryptSpreadComm in config params when vertica version is old.
		// EncryptSpreadComm will be set using DDL after db is created.
		vdb.Annotations[vmeta.VersionAnnotation] = "v12.0.3"
		Expect(g.Setup()).Should(Succeed())
		// default value
		g.SetEncryptSpreadCommConfigIfNecessary()
		Expect(g.ConfigurationParams.Size()).Should(Equal(0))
		// empty string
		g.Vdb.Spec.EncryptSpreadComm = ""
		g.SetEncryptSpreadCommConfigIfNecessary()
		Expect(g.ConfigurationParams.Size()).Should(Equal(0))
		// "vertica"
		g.Vdb.Spec.EncryptSpreadComm = vapi.EncryptSpreadCommWithVertica
		g.SetEncryptSpreadCommConfigIfNecessary()
		Expect(g.ConfigurationParams.Size()).Should(Equal(0))
		// "diabled"
		g.Vdb.Spec.EncryptSpreadComm = vapi.EncryptSpreadCommDisabled
		g.SetEncryptSpreadCommConfigIfNecessary()
		Expect(g.ConfigurationParams.Size()).Should(Equal(0))
	})
})
