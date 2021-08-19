/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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
	"io/ioutil"
	"os"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("cluster", func() {
	var logger logr.Logger
	vdb := vapi.MakeVDB()
	prunner := &cmds.FakePodRunner{Results: cmds.CmdResults{}}

	It("should create admintools.conf with a single host", func() {
		w := MakeClusterWriter(logger, vdb, prunner)
		cnts, err := genAtConf(w, types.NamespacedName{}, []string{"10.1.1.1"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.1"))
		Expect(cnts).Should(ContainSubstring("node0001 = 10.1.1.1,"))
	})

	It("should append hosts to an existing admintools.conf file", func() {
		w := MakeClusterWriter(logger, vdb, prunner)
		cnts, err := genAtConf(w, types.NamespacedName{}, []string{"10.1.1.1"})
		Expect(err).Should(Succeed())
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		prunner.Results[pn] = []cmds.CmdResult{
			{Stdout: cnts},
		}
		cnts, err = genAtConf(w, pn, []string{"10.1.1.2"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.1,10.1.1.2"))
		Expect(cnts).Should(ContainSubstring("node0001 = 10.1.1.1,"))
		Expect(cnts).Should(ContainSubstring("node0002 = 10.1.1.2,"))
	})

	It("should treat dup IPs as a no-op", func() {
		w := MakeClusterWriter(logger, vdb, prunner)
		cnts, err := genAtConf(w, types.NamespacedName{}, []string{"10.1.1.1", "10.1.1.2"})
		Expect(err).Should(Succeed())
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		prunner.Results[pn] = []cmds.CmdResult{
			{Stdout: cnts},
		}
		cnts, err = genAtConf(w, pn, []string{"10.1.1.2", "10.1.1.3"})
		Expect(err).Should(Succeed())
		Expect(cnts).Should(ContainSubstring("hosts = 10.1.1.1,10.1.1.2,10.1.1.3"))
		Expect(cnts).Should(ContainSubstring("node0001 = 10.1.1.1,"))
		Expect(cnts).Should(ContainSubstring("node0002 = 10.1.1.2,"))
		Expect(cnts).Should(ContainSubstring("node0003 = 10.1.1.3,"))
	})
})

// genAtConf is a helper that will generate a new admintools.conf with the given IPs in it
func genAtConf(w Writer, pn types.NamespacedName, ips []string) (string, error) {
	fn, err := w.AddHosts(context.TODO(), pn, ips)
	defer os.Remove(fn)
	if err != nil {
		return "", err
	}
	rawCnts, err := ioutil.ReadFile(fn)
	if err != nil {
		return "", err
	}
	return string(rawCnts), nil
}
