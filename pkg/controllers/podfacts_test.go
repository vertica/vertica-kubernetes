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

package controllers

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"k8s.io/apimachinery/pkg/types"
	"yunion.io/x/pkg/tristate"
)

var _ = Describe("podfacts", func() {
	ctx := context.Background()
	It("should not fail when collecting facts on an non-existent pod", func() {
		vdb := vapi.MakeVDB()

		sc := &vdb.Spec.Subclusters[0]
		fpr := &cmds.FakePodRunner{}
		pfacts := &PodFacts{Client: k8sClient, PRunner: fpr, Detail: make(PodFactDetail)}
		Expect(pfacts.collectPodByStsIndex(ctx, vdb, sc, 0)).Should(Succeed())
		podName := names.GenPodName(vdb, sc, 0)
		f, ok := (pfacts.Detail[podName])
		Expect(ok).Should(BeTrue())
		Expect(f.isPodRunning).Should(BeFalse())
	})

	It("should detect that there is a stale admintools.conf", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		sc := &vdb.Spec.Subclusters[0]
		installIndFn := paths.GenInstallerIndicatorFileName(vdb)
		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{
			names.GenPodName(vdb, sc, 0): []cmds.CmdResult{
				{Stderr: "cat: " + installIndFn + ": No such file or directory", Err: errors.New("file not found")},
			},
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{
				{Stderr: "cat: " + installIndFn + ": No such file or directory", Err: errors.New("file not found")},
				{Stderr: "No such file or directory", Err: errors.New("no such file")},
			},
		}}
		pfacts := &PodFacts{Client: k8sClient, PRunner: fpr, Detail: make(PodFactDetail)}
		Expect(pfacts.collectPodByStsIndex(ctx, vdb, sc, 0)).Should(Succeed())
		pod0 := names.GenPodName(vdb, sc, 0)
		f, ok := (pfacts.Detail[pod0])
		Expect(ok).Should(BeTrue())
		Expect(f.isPodRunning).Should(BeTrue())
		Expect(f.isInstalled.IsFalse()).Should(BeTrue())
		Expect(f.hasStaleAdmintoolsConf).Should(BeTrue())
		Expect(pfacts.collectPodByStsIndex(ctx, vdb, sc, 1)).Should(Succeed())
		pod1 := names.GenPodName(vdb, sc, 1)
		f, ok = (pfacts.Detail[pod1])
		Expect(ok).Should(BeTrue())
		Expect(f.isPodRunning).Should(BeTrue())
		Expect(f.isInstalled.IsFalse()).Should(BeTrue())
		Expect(f.hasStaleAdmintoolsConf).Should(BeFalse())
	})

	It("should never indicate db exists if pods not running", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsNotRunning)
		defer deletePods(ctx, vdb)

		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pf, ok := pfacts.Detail[nm]
		Expect(ok).Should(BeTrue())
		Expect(pf.isInstalled).Should(Equal(tristate.None))
		Expect(pf.dbExists).Should(Equal(tristate.None))
	})

	It("should not indicate db exists if db directory is not there", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{
			nm: []cmds.CmdResult{
				{}, // admintools.conf exists
				{Stderr: "No such file or directory", Err: errors.New("file not found")}, // db dir does not
			},
		}}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pf, ok := pfacts.Detail[nm]
		Expect(ok).Should(BeTrue())
		Expect(pf.isInstalled).Should(Equal(tristate.True))
		Expect(pf.dbExists).Should(Equal(tristate.False))
		Expect(pfacts.doesDBExist()).Should(Equal(tristate.True))
	})

	It("should verify all doesDBExist return codes", func() {
		pf := MakePodFacts(k8sClient, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{dbExists: tristate.False, isPodRunning: true}
		Expect(pf.doesDBExist()).Should(Equal(tristate.False))
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{dbExists: tristate.False, isPodRunning: false}
		Expect(pf.doesDBExist()).Should(Equal(tristate.None))
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{dbExists: tristate.True, isPodRunning: true}
		Expect(pf.doesDBExist()).Should(Equal(tristate.True))
	})

	It("should verify anyPodsMissingDB return codes", func() {
		pf := MakePodFacts(k8sClient, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{dbExists: tristate.True, subcluster: "sc1"}
		Expect(pf.anyPodsMissingDB("sc1")).Should(Equal(tristate.False))
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{dbExists: tristate.None, subcluster: "sc1"}
		Expect(pf.anyPodsMissingDB("sc1")).Should(Equal(tristate.None))
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{dbExists: tristate.False, isPodRunning: true, subcluster: "sc1"}
		Expect(pf.anyPodsMissingDB("sc1")).Should(Equal(tristate.True))
		pf.Detail[types.NamespacedName{Name: "p4"}] = &PodFact{dbExists: tristate.True, isPodRunning: true, subcluster: "sc2"}
		Expect(pf.anyPodsMissingDB("sc2")).Should(Equal(tristate.False))
	})

	It("should verify return of findPodsWithMissingDB", func() {
		pf := MakePodFacts(k8sClient, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", subcluster: "sc1", dbExists: tristate.True,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", subcluster: "sc1", dbExists: tristate.False, isPodRunning: true,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", subcluster: "sc1", dbExists: tristate.False, isPodRunning: false,
		}
		pf.Detail[types.NamespacedName{Name: "p4"}] = &PodFact{
			dnsName: "p4", subcluster: "sc2", dbExists: tristate.False, isPodRunning: true,
		}
		pf.Detail[types.NamespacedName{Name: "p5"}] = &PodFact{
			dnsName: "p5", subcluster: "sc2", dbExists: tristate.False, isPodRunning: true,
		}
		pods := pf.findPodsWithMissingDB("sc1")
		Expect(len(pods)).Should(Equal(1))
		Expect(pods[0].dnsName).Should(Equal("p2"))
		pods = pf.findPodsWithMissingDB("sc2")
		Expect(len(pods)).Should(Equal(2))
		Expect(pods[0].dnsName).Should(Equal("p4"))
		Expect(pods[1].dnsName).Should(Equal("p5"))
	})

	It("should parse the vertica node name from the directory listing", func() {
		Expect(parseVerticaNodeName("data/1b532ad7-42bf-4777-a6d1-fdae69fb94de/vertdb/v_vertdb_node0001_data/")).Should(
			Equal("v_vertdb_node0001"))
		Expect(parseVerticaNodeName("vertdb/v_thedb_node0109_data/")).Should(Equal("v_thedb_node0109"))
		Expect(parseVerticaNodeName("vertdb/v_db2_node8844_data/")).Should(Equal("v_db2_node8844"))
	})

	It("should mark db is down if vsql fails", func() {
		vdb := vapi.MakeVDB()
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{
			Results: cmds.CmdResults{
				pn: []cmds.CmdResult{
					{Err: errors.New("vsql failed"), Stderr: "vsql: could not connect to server: Connection refused"},
				},
			},
		}
		pfs := MakePodFacts(k8sClient, fpr)
		pf := &PodFact{name: pn, isPodRunning: true}
		Expect(pfs.checkIfNodeIsUp(ctx, vdb, pf)).Should(Succeed())
		Expect(pf.upNode).Should(BeFalse())
	})

	It("should mark db is up if vsql succeeds", func() {
		vdb := vapi.MakeVDB()
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{
			Results: cmds.CmdResults{
				pn: []cmds.CmdResult{
					{Stdout: " ?column? \n----------\n        1\n(1 row)\n\n"},
				},
			},
		}
		pfs := MakePodFacts(k8sClient, fpr)
		pf := &PodFact{name: pn, isPodRunning: true}
		Expect(pfs.checkIfNodeIsUp(ctx, vdb, pf)).Should(Succeed())
		Expect(pf.upNode).Should(BeTrue())
	})

	It("should fail if checkIfNodeIsUp gets an unexpected error", func() {
		vdb := vapi.MakeVDB()
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{
			Results: cmds.CmdResults{
				pn: []cmds.CmdResult{
					{Err: errors.New("unexpected error"), Stderr: "unknown error"},
				},
			},
		}
		pfs := MakePodFacts(k8sClient, fpr)
		pf := &PodFact{name: pn, isPodRunning: true}
		Expect(pfs.checkIfNodeIsUp(ctx, vdb, pf)).ShouldNot(Succeed())
	})

	It("should parse out the compat21 node name from install indicator file", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{
			nm: []cmds.CmdResult{
				{Stdout: "node0010\n"}, // install indicator contents
			},
		}}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pf, ok := pfacts.Detail[nm]
		Expect(ok).Should(BeTrue())
		Expect(pf.isInstalled).Should(Equal(tristate.True))
		Expect(pf.compat21NodeName).Should(Equal("node0010"))
	})
})
