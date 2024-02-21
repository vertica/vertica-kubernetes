/*
 (c) Copyright [2021-2024] Open Text.
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

package atconf

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("file_writer", func() {
	var logger logr.Logger
	vdb := vapi.MakeVDB()
	prunner := &cmds.FakePodRunner{Results: cmds.CmdResults{}}

	It("should create admintools.conf with a single host", func() {
		w := MakeFileWriter(logger, vdb, prunner)
		cnts, err := genAtConfWithAdd(w, types.NamespacedName{}, []string{"10.1.1.1"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.1"))
		Expect(cnts).Should(ContainSubstring("node0001 = 10.1.1.1,"))
	})

	It("should append hosts to an existing admintools.conf file", func() {
		w := MakeFileWriter(logger, vdb, prunner)
		cnts, err := genAtConfWithAdd(w, types.NamespacedName{}, []string{"10.1.1.1"})
		Expect(err).Should(Succeed())
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		prunner.Results[pn] = []cmds.CmdResult{
			{Stdout: cnts},
		}
		cnts, err = genAtConfWithAdd(w, pn, []string{"10.1.1.2"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.1,10.1.1.2"))
		Expect(cnts).Should(ContainSubstring("node0001 = 10.1.1.1,"))
		Expect(cnts).Should(ContainSubstring("node0002 = 10.1.1.2,"))
	})

	It("should treat dup IPs as a no-op", func() {
		w := MakeFileWriter(logger, vdb, prunner)
		cnts, err := genAtConfWithAdd(w, types.NamespacedName{}, []string{"10.1.1.1", "10.1.1.2"})
		Expect(err).Should(Succeed())
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		prunner.Results[pn] = []cmds.CmdResult{
			{Stdout: cnts},
		}
		cnts, err = genAtConfWithAdd(w, pn, []string{"10.1.1.2", "10.1.1.3"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.1,10.1.1.2,10.1.1.3"))
		Expect(cnts).Should(ContainSubstring("node0001 = 10.1.1.1,"))
		Expect(cnts).Should(ContainSubstring("node0002 = 10.1.1.2,"))
		Expect(cnts).Should(ContainSubstring("node0003 = 10.1.1.3,"))
	})

	It("should set ipv6 flag appropriately", func() {
		w := MakeFileWriter(logger, vdb, prunner)
		cnts, err := genAtConfWithAdd(w, types.NamespacedName{}, []string{"10.1.1.1"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("ipv6 = False"))

		cnts, err = genAtConfWithAdd(w, types.NamespacedName{}, []string{"2001:0db8:85a3:0000:0000:8a2e:0370:7334"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("ipv6 = True"))
	})

	It("should be able to remove a single IP", func() {
		w := MakeFileWriter(logger, vdb, prunner)
		cnts, err := genAtConfWithAdd(w, types.NamespacedName{}, []string{"10.1.1.1", "10.1.1.2", "10.1.1.3"})
		Expect(err).Should(Succeed())
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		prunner.Results[pn] = []cmds.CmdResult{
			{Stdout: cnts},
		}
		cnts, err = genAtConfWithDel(w, pn, []string{"10.1.1.2"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.1,10.1.1.3"))
		Expect(cnts).Should(ContainSubstring("node0001 = 10.1.1.1,"))
		Expect(cnts).ShouldNot(ContainSubstring("node0002 = 10.1.1.2,"))
		Expect(cnts).Should(ContainSubstring("node0003 = 10.1.1.3,"))
	})

	It("should be able to remove multiple IPs at a time", func() {
		w := MakeFileWriter(logger, vdb, prunner)
		cnts, err := genAtConfWithAdd(w, types.NamespacedName{}, []string{"10.1.1.1", "10.1.1.2", "10.1.1.3"})
		Expect(err).Should(Succeed())
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		prunner.Results[pn] = []cmds.CmdResult{
			{Stdout: cnts},
		}
		cnts, err = genAtConfWithDel(w, pn, []string{"10.1.1.2", "10.1.1.1"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.3"))
		Expect(cnts).ShouldNot(ContainSubstring("node0001 = 10.1.1.1,"))
		Expect(cnts).ShouldNot(ContainSubstring("node0002 = 10.1.1.2,"))
		Expect(cnts).Should(ContainSubstring("node0003 = 10.1.1.3,"))
	})

	It("should be able to handle when IPs are a subset of others", func() {
		w := MakeFileWriter(logger, vdb, prunner)
		cnts, err := genAtConfWithAdd(w, types.NamespacedName{}, []string{"10.1.1.1", "10.1.1.10", "10.1.1.11"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.1,10.1.1.10,10.1.1.11"))
		Expect(cnts).Should(ContainSubstring("node0001 = 10.1.1.1,"))
		Expect(cnts).Should(ContainSubstring("node0002 = 10.1.1.10,"))
		Expect(cnts).Should(ContainSubstring("node0003 = 10.1.1.11,"))
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		prunner.Results[pn] = []cmds.CmdResult{
			{Stdout: cnts},
		}
		cnts, err = genAtConfWithDel(w, pn, []string{"10.1.1.1"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.10,10.1.1.11"))
		Expect(cnts).ShouldNot(ContainSubstring("node0001 = 10.1.1.1,"))
		Expect(cnts).Should(ContainSubstring("node0002 = 10.1.1.10,"))
		Expect(cnts).Should(ContainSubstring("node0003 = 10.1.1.11,"))
	})

	It("should be able to setup conf with separate catalog path", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Local.CatalogPath = "/catalog"
		w := MakeFileWriter(logger, vdb, prunner)
		cnts, err := genAtConfWithAdd(w, types.NamespacedName{}, []string{"10.1.1.1"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring(fmt.Sprintf("%s,%s", vdb.Spec.Local.CatalogPath, vdb.Spec.Local.DataPath)))
	})

	It("should reuse node names", func() {
		vdb := vapi.MakeVDB()
		w := &FileWriter{Log: logger, Vdb: vdb, PRunner: prunner}
		Expect(w.createAdmintoolsConfBase(context.TODO(), types.NamespacedName{})).Should(Succeed())
		Expect(w.loadATConf()).Should(Succeed())
		Expect(w.addNewHosts([]string{"10.1.1.1", "10.1.1.2"})).Should(Succeed())
		// Make node0001 available for reuse
		Expect(w.removeNodes([]string{"10.1.1.1"})).Should(Succeed())
		Expect(w.addNewHosts([]string{"10.1.1.3", "10.1.1.4"})).Should(Succeed())
		fn, err := w.saveATConf()
		defer os.Remove(fn)
		Expect(err).Should(Succeed())
		rawCnts, err := os.ReadFile(fn)
		Expect(err).Should(Succeed())
		s := string(rawCnts)
		Expect(s).Should(ContainSubstring("node0002 = 10.1.1.2"))
		Expect(s).Should(ContainSubstring("node0001 = 10.1.1.3"))
		Expect(s).Should(ContainSubstring("node0003 = 10.1.1.4"))
	})
})

// genAtConfWithAdd is a helper that will generate a new admintools.conf with the given IPs in it
func genAtConfWithAdd(w Writer, pn types.NamespacedName, ips []string) (string, error) {
	fn, err := w.AddHosts(context.TODO(), pn, ips)
	defer os.Remove(fn)
	if err != nil {
		return "", err
	}
	rawCnts, err := os.ReadFile(fn)
	if err != nil {
		return "", err
	}
	return string(rawCnts), nil
}

// genAtConfWithDel is a helper that will generate a new admintools.conf with the given IPs removed
func genAtConfWithDel(w Writer, pn types.NamespacedName, ips []string) (string, error) {
	fn, err := w.RemoveHosts(context.TODO(), pn, ips)
	defer os.Remove(fn)
	if err != nil {
		return "", err
	}
	rawCnts, err := os.ReadFile(fn)
	if err != nil {
		return "", err
	}
	return string(rawCnts), nil
}
