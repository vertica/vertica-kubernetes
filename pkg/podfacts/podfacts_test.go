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

package podfacts

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
)

const TestPassword = "test-pw"

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
		sc.Size = 1
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		nm := names.GenPodName(vdb, sc, 0)
		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: sc.Name, AddedToDBCount: sc.Size, Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
		}
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pf, ok := pfacts.Detail[nm]
		Expect(ok).Should(BeTrue())
		Expect(pf.isInstalled).Should(BeTrue())
		Expect(pf.dbExists).Should(BeTrue())

		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: sc.Name, AddedToDBCount: 0, Detail: []vapi.VerticaDBPodStatus{{Installed: false, AddedToDB: false}}},
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
			{Name: vdb.Spec.Subclusters[0].Name, Detail: []vapi.VerticaDBPodStatus{{Installed: true}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pf, ok := pfacts.Detail[nm]
		Expect(ok).Should(BeTrue())
		Expect(pf.isInstalled).Should(BeTrue())
	})

	It("should verify all doesDBExist return codes", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{dbExists: false, isPodRunning: true, isPrimary: true}
		Expect(pf.DoesDBExist()).Should(BeFalse())
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{dbExists: false, isPodRunning: false, isPrimary: true}
		Expect(pf.DoesDBExist()).Should(BeFalse())
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{dbExists: true, isPodRunning: true, isPrimary: true}
		Expect(pf.DoesDBExist()).Should(BeTrue())
		// Change pf to be a secondary
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{dbExists: true, isPodRunning: true, isPrimary: false}
		Expect(pf.DoesDBExist()).Should(BeFalse())
	})

	It("should verify return of CountNotReadOnlyWithOldImage", func() {
		const OldImage = "image:v1"
		const NewImage = "image:v2"
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
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
		Expect(pf.CountNotReadOnlyWithOldImage(NewImage)).Should(Equal(1))
		pf.Detail[types.NamespacedName{Name: "p1"}].readOnly = true
		Expect(pf.CountNotReadOnlyWithOldImage(NewImage)).Should(Equal(0))
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
		pfs := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		pf := &PodFact{name: pn, isPodRunning: true}
		Expect(pfs.checkNodeDetails(ctx, vdb, pf, &GatherState{})).Should(Succeed())
		Expect(pf.upNode).Should(BeFalse())
	})

	It("should mark db is up if vertica PID is running", func() {
		vdb := vapi.MakeVDB()
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{}
		pfs := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		pf := &PodFact{name: pn, isPodRunning: true, dbExists: true}
		gs := &GatherState{VerticaPIDRunning: true}
		Expect(pfs.checkForSimpleGatherStateMapping(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.upNode).Should(BeTrue())
	})

	It("checkIfNodeIsUpAndReadOnly should check for read-only on 11.0.2 servers", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VersionAnnotation] = vapi.NodesHaveReadOnlyStateVersion
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr := &cmds.FakePodRunner{
			Results: cmds.CmdResults{
				pn: []cmds.CmdResult{
					// node_name|node_state|is_primary|subcluster_oid|is_readonly
					{Stdout: "v_db_node0001|UP|t|123456|t"},
				},
			},
		}
		pfs := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		pf := &PodFact{name: pn, isPodRunning: true, dbExists: true}
		gs := &GatherState{VerticaPIDRunning: true}
		Expect(pfs.checkForSimpleGatherStateMapping(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pfs.checkNodeDetails(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.isPrimary).Should(BeTrue())
		Expect(pf.upNode).Should(BeTrue())
		Expect(pf.readOnly).Should(BeTrue())
	})

	It("should detect startup in progress correctly", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		pn := names.GenPodName(vdb, sc, 0)
		fpr := &cmds.FakePodRunner{}
		pfs := MakePodFacts(vdbRec, fpr, logger, TestPassword)
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
					{Stdout: "v_db_node0001|UP|t||f"},
				},
			},
		}
		pfs := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		gs := &GatherState{VerticaPIDRunning: true}
		pf := &PodFact{name: pn, isPodRunning: true, dbExists: true}
		Expect(pfs.checkForSimpleGatherStateMapping(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pfs.checkNodeDetails(ctx, vdb, pf, gs)).Should(Succeed())
		Expect(pf.isPrimary).Should(BeTrue())
		Expect(pf.upNode).Should(BeTrue())
		Expect(pf.readOnly).Should(BeFalse())
		Expect(pf.subclusterOid).Should(Equal(""))
	})

	It("should return consistent first pod", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true,
		}
		p, ok := pf.FindFirstPodSorted(func(p *PodFact) bool { return true })
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p1"))
	})

	It("should return filtered pods in vnode sort order", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
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
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true, upNode: true, readOnly: false, isPendingDelete: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true, upNode: true, readOnly: true, isPendingDelete: false,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true, upNode: true, readOnly: false, isPendingDelete: false,
		}
		p, ok := pf.FindPodToRunAdminCmdAny()
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p3"))

		By("finding up and not read-only")
		pf = MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true, upNode: true, readOnly: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true, upNode: true, readOnly: false,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true, upNode: true, readOnly: false,
		}
		p, ok = pf.FindPodToRunAdminCmdAny()
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p2"))

		By("finding up and read-only")
		pf = MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", dbExists: true, upNode: false, readOnly: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", dbExists: true, upNode: true, readOnly: true,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", dbExists: true, upNode: true, readOnly: true,
		}
		p, ok = pf.FindPodToRunAdminCmdAny()
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p2"))

		By("finding a pod with an install")
		pf = MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
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
		p, ok = pf.FindPodToRunAdminCmdAny()
		Expect(ok).Should(BeTrue())
		Expect(p.dnsName).Should(Equal("p2"))
	})

	It("should correctly return re-ip pods", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			dnsName: "p1", vnodeName: "node1", dbExists: true, exists: true, isPodRunning: true, isInstalled: true,
			hasNMASidecar: true, isNMAContainerReady: true,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			dnsName: "p2", vnodeName: "node2", dbExists: false, exists: true, isPodRunning: true, isInstalled: true,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			dnsName: "p3", vnodeName: "node3", dbExists: false, exists: true, isPodRunning: true, isInstalled: false,
		}
		pf.Detail[types.NamespacedName{Name: "p4"}] = &PodFact{
			dnsName: "p4", vnodeName: "node4", dbExists: true, exists: true, isPodRunning: true, isInstalled: false,
			hasNMASidecar: true, isNMAContainerReady: false,
		}
		verifyReIP(&pf)
	})

	It("should detect when the vdb has changed since collection", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		Ω(pf.Collect(ctx, vdb)).Should(Succeed())
		Ω(pf.HasVerticaDBChangedSinceCollection(ctx, vdb)).Should(BeFalse())

		// Mock a change by adding an annotation
		if vdb.Annotations == nil {
			vdb.Annotations = make(map[string]string)
		}
		vdb.Annotations["foo"] = "bar"
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())
		Ω(pf.HasVerticaDBChangedSinceCollection(ctx, vdb)).Should(BeTrue())
	})

	It("should do quorum check correctly", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			isPrimary: true, dbExists: true, hasDCTableAnnotations: true, isPodRunning: true, upNode: false,
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			isPrimary: true, dbExists: true, hasDCTableAnnotations: true, isPodRunning: true, upNode: false,
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			isPrimary: false, dbExists: true, hasDCTableAnnotations: true, isPodRunning: true, upNode: false,
		}
		pf.Detail[types.NamespacedName{Name: "p4"}] = &PodFact{
			isPrimary: true, dbExists: false, hasDCTableAnnotations: true, isPodRunning: true, upNode: false,
		}
		pf.Detail[types.NamespacedName{Name: "p5"}] = &PodFact{
			isPrimary: true, dbExists: false, hasDCTableAnnotations: true, isPodRunning: true, upNode: false,
		}

		// 4 primary nodes, 2 restartable primary nodes
		result := pf.QuorumCheckForRestartCluster(true)
		Expect(result).Should(BeFalse())
		// 5 primary nodes, 3 restartable primary nodes
		pf.Detail[types.NamespacedName{Name: "p3"}].isPrimary = true
		result = pf.QuorumCheckForRestartCluster(true)
		Expect(result).Should(BeTrue())
		// 5 primary nodes, 2 restartable primary nodes
		pf.Detail[types.NamespacedName{Name: "p3"}].dbExists = false
		result = pf.QuorumCheckForRestartCluster(true)
		Expect(result).Should(BeFalse())
	})

	It("should find sandbox initiator correctly", func() {
		pf := MakePodFacts(vdbRec, &cmds.FakePodRunner{}, logger, TestPassword)
		pf.Detail[types.NamespacedName{Name: "p1"}] = &PodFact{
			isPrimary: false, subclusterName: "sc1", upNode: true, sandbox: "sand1", podIP: "1.1.1.1",
		}
		pf.Detail[types.NamespacedName{Name: "p2"}] = &PodFact{
			isPrimary: true, subclusterName: "sc2", upNode: true, sandbox: "sand1", podIP: "2.2.2.2",
		}
		pf.Detail[types.NamespacedName{Name: "p3"}] = &PodFact{
			isPrimary: true, subclusterName: "sc3", upNode: true, sandbox: "sand1", podIP: "3.3.3.3",
		}
		pf.Detail[types.NamespacedName{Name: "p4"}] = &PodFact{
			isPrimary: true, subclusterName: "sc4", upNode: true, sandbox: "sand2", podIP: "4.4.4.4",
		}
		// should get a primary up node ip not in sc2
		initiatorIP, ok := pf.GetInitiatorIPInSB("sand1", "sc2")
		Expect(initiatorIP).Should(Equal("3.3.3.3"))
		Expect(ok).Should(BeTrue())
		// should not get an ip
		initiatorIP, ok = pf.GetInitiatorIPInSB("sand2", "sc4")
		Expect(initiatorIP).Should(Equal(""))
		Expect(ok).Should(BeFalse())
	})
})

func verifyReIP(pf *PodFacts) {
	By("finding any installed pod")
	pods := pf.FindReIPPods(DBCheckAny)
	Ω(pods).Should(HaveLen(2))
	Ω(pods[0].dnsName).Should(Equal("p1"))
	Ω(pods[1].dnsName).Should(Equal("p2"))

	By("finding pods with a db")
	pods = pf.FindReIPPods(DBCheckOnlyWithDBs)
	Ω(pods).Should(HaveLen(1))
	Ω(pods[0].dnsName).Should(Equal("p1"))

	By("finding pods without a db")
	pods = pf.FindReIPPods(DBCheckOnlyWithoutDBs)
	Ω(pods).Should(HaveLen(1))
	Ω(pods[0].dnsName).Should(Equal("p2"))
}
