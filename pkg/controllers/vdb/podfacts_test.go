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
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("podfacts", func() {
	ctx := context.Background()
	It("should not fail when collecting facts on an non-existent pod", func() {
		vdb := vapi.MakeVDB()

		sc := &vdb.Spec.Subclusters[0]
		fpr := &cmds.FakePodRunner{}
		pfacts := &PodFacts{VRec: vdbRec, PRunner: fpr, Detail: make(PodFactDetail)}
		Expect(pfacts.collectPodByStsIndex(ctx, vdb, sc, &appsv1.StatefulSet{}, 0)).Should(Succeed())
		podName := names.GenPodName(vdb, sc, 0)
		f, ok := (pfacts.Detail[podName])
		Expect(ok).Should(BeTrue())
		Expect(f.isPodRunning).Should(BeFalse())
	})

	It("should use status fields to check if db exists when pods aren't running", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		nm := names.GenPodName(vdb, sc, 0)
		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: sc.Name, InstallCount: sc.Size, AddedToDBCount: sc.Size},
		}
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pf, ok := pfacts.Detail[nm]
		Expect(ok).Should(BeTrue())
		Expect(pf.isInstalled).Should(BeTrue())
		Expect(pf.dbExists).Should(BeTrue())

		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: sc.Name, InstallCount: 0, AddedToDBCount: 0},
		}
		pfacts.Invalidate()
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pf, ok = pfacts.Detail[nm]
		Expect(ok).Should(BeTrue())
		Expect(pf.isInstalled).Should(BeFalse())
		Expect(pf.dbExists).Should(BeFalse())
	})

	It("should indicate installation if pod not running but status has been updated", func() {
		vdb := vapi.MakeVDB()
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: vdb.Spec.Subclusters[0].Name, InstallCount: 1},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pf, ok := pfacts.Detail[nm]
		Expect(ok).Should(BeTrue())
		Expect(pf.isInstalled).Should(BeTrue())
	})

	It("should verify all doesDBExist return codes", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{dbExists: false, isPodRunning: true, isPrimary: true}
		Expect(pf.doesDBExist()).Should(BeFalse())
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{dbExists: false, isPodRunning: false, isPrimary: true}
		Expect(pf.doesDBExist()).Should(BeFalse())
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{dbExists: true, isPodRunning: true, isPrimary: true}
		Expect(pf.doesDBExist()).Should(BeTrue())
		// Change pf to be a secondary
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{dbExists: true, isPodRunning: true, isPrimary: false}
		Expect(pf.doesDBExist()).Should(BeFalse())
	})

	It("should verify return of countNotReadOnlyWithOldImage", func() {
		const OldImage = "image:v1"
		const NewImage = "image:v2"
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			isPodRunning: true,
			upNode:       true,
			readOnly:     false,
			image:        OldImage,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			isPodRunning: true,
			upNode:       true,
			readOnly:     true,
			image:        OldImage,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			isPodRunning: true,
			upNode:       true,
			readOnly:     false,
			image:        NewImage,
		}
		Expect(pf.countNotReadOnlyWithOldImage(NewImage)).Should(Equal(1))
		pf.Detail[types.NamespacedName{Name: "p1"}].readOnly = true
		Expect(pf.countNotReadOnlyWithOldImage(NewImage)).Should(Equal(0))
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
		pfs := MakePodFacts(vdbRec, fpr)
		pf := &PodFact{name: pn, isPodRunning: true}
		Expect(pfs.checkNodeStatus(ctx, vdb, pf, &GatherState{})).Should(Succeed())
		Expect(pf.upNode).Should(BeFalse())
	})

	It("should mark db is up if vertica PID is running", func() {
		vdb := vapi.MakeVDB()
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{}
		pfs := MakePodFacts(vdbRec, fpr)
		pf := &PodFact{name: pn, isPodRunning: true, dbExists: true}
		gs := &GatherState{VerticaPIDRunning: true}
		Expect(pfs.checkForSimpleGatherStateMapping(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.upNode).Should(BeTrue())
	})

	It("checkIfNodeIsUpAndReadOnly should check for read-only on 11.0.2 servers", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vapi.VersionAnnotation] = vapi.NodesHaveReadOnlyStateVersion
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{
			Results: cmds.CmdResults{
				pn: []cmds.CmdResult{
					{Stdout: "v_db_node0001|UP|123456|t"},
				},
			},
		}
		pfs := MakePodFacts(vdbRec, fpr)
		pf := &PodFact{name: pn, isPodRunning: true, dbExists: true}
		gs := &GatherState{VerticaPIDRunning: true}
		Expect(pfs.checkForSimpleGatherStateMapping(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pfs.checkNodeStatus(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.upNode).Should(BeTrue())
		Expect(pf.readOnly).Should(BeTrue())
	})

	It("should parse read-only state from node query", func() {
		readOnly1, oid1, err := parseNodeStateAndReadOnly("v_db_node0001|UP|123456|t\n")
		Expect(err).Should(Succeed())
		Expect(readOnly1).Should(BeTrue())
		Expect(oid1).Should(Equal("123456"))

		readOnly2, oid2, err := parseNodeStateAndReadOnly("v_db_node0001|UP|7890123|f\n")
		Expect(err).Should(Succeed())
		Expect(readOnly2).Should(BeFalse())
		Expect(oid2).Should(Equal("7890123"))

		readOnly3, oid3, err := parseNodeStateAndReadOnly("v_db_node0001|UP|456789\n")
		Expect(err).Should(Succeed())
		Expect(readOnly3).Should(BeFalse())
		Expect(oid3).Should(Equal("456789"))

		_, _, err = parseNodeStateAndReadOnly("")
		Expect(err).Should(Succeed())

		_, _, err = parseNodeStateAndReadOnly("v_db_node0001|UP|123|t|garbage")
		Expect(err).Should(Succeed())
	})

	It("should parse node subscriptions output", func() {
		pf := &PodFact{}
		Expect(setShardSubscription("3\n", pf)).Should(Succeed())
		Expect(pf.shardSubscriptions).Should(Equal(3))
	})

	It("should parse depot details output", func() {
		pf := &PodFact{}
		Expect(pf.setDepotDetails("1248116736|60%\n")).Should(Succeed())
		Expect(pf.maxDepotSize).Should(Equal(1248116736))
		Expect(pf.depotDiskPercentSize).Should(Equal("60%"))
		Expect(pf.setDepotDetails("3248116736|\n")).Should(Succeed())
		Expect(pf.maxDepotSize).Should(Equal(3248116736))
		Expect(pf.depotDiskPercentSize).Should(Equal(""))
		Expect(pf.setDepotDetails("a|b|c")).ShouldNot(Succeed())
		Expect(pf.setDepotDetails("not-a-number|blah")).ShouldNot(Succeed())
	})

	It("should detect startup in progress correctly", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		pn := names.GenPodName(vdb, sc, 0)
		fpr := &cmds.FakePodRunner{}
		pfs := MakePodFacts(vdbRec, fpr)
		pf := &PodFact{name: pn, isPodRunning: true, dbExists: true}
		gs := &GatherState{VerticaPIDRunning: true, StartupComplete: false}
		Expect(pfs.checkIfNodeIsDoingStartup(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.startupInProgress).Should(BeTrue())

		pf = &PodFact{name: pn, isPodRunning: true, dbExists: true}
		gs = &GatherState{VerticaPIDRunning: false, StartupComplete: true}
		Expect(pfs.checkIfNodeIsDoingStartup(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.startupInProgress).Should(BeFalse())

		pf = &PodFact{name: pn, isPodRunning: true, dbExists: true}
		gs = &GatherState{VerticaPIDRunning: true, StartupComplete: true}
		Expect(pfs.checkIfNodeIsDoingStartup(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.startupInProgress).Should(BeFalse())
	})

	It("should handle subcluster oid lookup properly for enterprise db", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.ShardCount = 0
		vdb.Spec.Communal.Path = ""

		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{
			Results: cmds.CmdResults{
				pn: []cmds.CmdResult{
					{Stdout: "v_db_node0001|UP||f"},
				},
			},
		}
		pfs := MakePodFacts(vdbRec, fpr)
		gs := &GatherState{VerticaPIDRunning: true}
		pf := &PodFact{name: pn, isPodRunning: true, dbExists: true}
		Expect(pfs.checkForSimpleGatherStateMapping(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pfs.checkNodeStatus(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.upNode).Should(BeTrue())
		Expect(pf.readOnly).Should(BeFalse())
		Expect(pf.subclusterOid).Should(Equal(""))
	})

	It("should return consistent first pod", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true,
		}
		p, ok := pf.findFirstPodSorted(func(p *PodFact) bool { return true })
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p1"))
	})

	It("should return filtered pods in vnode sort order", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true, vnodeName: "v_db_node0003",
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true, vnodeName: "v_db_node0002",
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true, vnodeName: "v_db_node0001",
		}
		pods := pf.filterPods(func(v *PodFact) bool { return true })
		Expect(len(pods)).Should(Equal(3))
		Expect(pods[0].dnsName).Should(Equal("p3"))
		Expect(pods[0].vnodeName).Should(Equal("v_db_node0001"))
		Expect(pods[1].dnsName).Should(Equal("p2"))
		Expect(pods[1].vnodeName).Should(Equal("v_db_node0002"))
		Expect(pods[2].dnsName).Should(Equal("p1"))
		Expect(pods[2].vnodeName).Should(Equal("v_db_node0003"))
	})

	It("should return correct pod in findPodToRunAdmintoolsAny", func() {
		By("finding up, not read-only and not pending delete")
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true, upNode: true, readOnly: false, pendingDelete: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true, upNode: true, readOnly: true, pendingDelete: false,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true, upNode: true, readOnly: false, pendingDelete: false,
		}
		p, ok := pf.findPodToRunAdminCmdAny()
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p3"))

		By("finding up and not read-only")
		pf = MakePodFacts(vdbRec, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true, upNode: true, readOnly: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true, upNode: true, readOnly: false,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true, upNode: true, readOnly: false,
		}
		p, ok = pf.findPodToRunAdminCmdAny()
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p2"))

		By("finding up and read-only")
		pf = MakePodFacts(vdbRec, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true, upNode: false, readOnly: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true, upNode: true, readOnly: true,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true, upNode: true, readOnly: true,
		}
		p, ok = pf.findPodToRunAdminCmdAny()
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p2"))

		By("finding a pod with an install")
		pf = MakePodFacts(vdbRec, &cmds.FakePodRunner{})
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", isInstalled: false, isPodRunning: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", isInstalled: true, isPodRunning: true,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", isInstalled: true, isPodRunning: false,
		}
		pf.Detail[types.NamespacedName{Name: "p4"}] = &PodFact{
			dnsName: "p3", isInstalled: true, isPodRunning: true,
		}
		p, ok = pf.findPodToRunAdminCmdAny()
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p2"))
	})
})
