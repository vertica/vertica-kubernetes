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

package vdb

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
)

var _ = Describe("subclustershutdown_reconciler", func() {
	It("should reconcile based on sandbox shutdown field", func() {
		const (
			maincluster = "main"
			subcluster1 = "sc1"
			subcluster2 = "sc2"
			subcluster3 = "sc3"
			subcluster4 = "sc4"
			sandbox1    = "sandbox1"
		)
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 4, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 3, Type: vapi.SecondarySubcluster},
			{Name: subcluster2, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster3, Size: 3, Type: vapi.SecondarySubcluster},
			{Name: subcluster4, Size: 3, Type: vapi.SecondarySubcluster},
		}
		// All subclusters are in main cluster.
		// Shutting down secondaries
		vdb.Spec.Subclusters[1].Shutdown = true
		vdb.Spec.Subclusters[3].Shutdown = true
		vdb.Spec.Subclusters[4].Shutdown = true
		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		upNodes := []uint{4, 3, 3, 3, 3}
		pfacts.ConstructsDetail(vdb.Spec.Subclusters, upNodes)
		Expect(len(pfacts.Detail)).Should(Equal(16))
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)

		act := MakeSubclusterShutdownReconciler(vdbRec, logger, vdb, dispatcher, &pfacts)
		r := act.(*SubclusterShutdownReconciler)
		scMap, err := r.getSubclustersToShutdown()
		Expect(err).Should(BeNil())
		Expect(len(scMap)).Should(Equal(3))
		subclusters := getSubclusters(scMap)
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[1].Name))
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[3].Name))
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[4].Name))

		// let's shut down a primary
		// We should still be good because 4 out of 7 primaries
		// will be up
		vdb.Spec.Subclusters[2].Shutdown = true
		upNodes = []uint{4, 3, 3, 3, 3}
		pfacts.ConstructsDetail(vdb.Spec.Subclusters, upNodes)
		act = MakeSubclusterShutdownReconciler(vdbRec, logger, vdb, dispatcher, &pfacts)
		r = act.(*SubclusterShutdownReconciler)
		scMap, err = r.getSubclustersToShutdown()
		Expect(err).Should(BeNil())
		Expect(len(scMap)).Should(Equal(4))
		subclusters = getSubclusters(scMap)
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[1].Name))
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[2].Name))
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[3].Name))
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[4].Name))

		// Let's try again but this time 3 out of 7 primaries
		// will be up so we should return an error
		upNodes = []uint{3, 3, 3, 3, 3}
		pfacts.ConstructsDetail(vdb.Spec.Subclusters, upNodes)
		act = MakeSubclusterShutdownReconciler(vdbRec, logger, vdb, dispatcher, &pfacts)
		r = act.(*SubclusterShutdownReconciler)
		_, err = r.getSubclustersToShutdown()
		Expect(err).ShouldNot(BeNil())

		// We sandbox 2 subclusters so they should be ignored
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Shutdown: true, Subclusters: []vapi.SubclusterName{{Name: subcluster1}, {Name: subcluster3}}},
		}
		upNodes = []uint{4, 3, 3, 3, 3}
		pfacts.ConstructsDetail(vdb.Spec.Subclusters, upNodes)
		act = MakeSubclusterShutdownReconciler(vdbRec, logger, vdb, dispatcher, &pfacts)
		r = act.(*SubclusterShutdownReconciler)
		scMap, err = r.getSubclustersToShutdown()
		Expect(err).Should(BeNil())
		Expect(len(scMap)).Should(Equal(2))
		subclusters = getSubclusters(scMap)
		Expect(subclusters).ShouldNot(ContainElement(vdb.Spec.Subclusters[1].Name))
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[2].Name))
		Expect(subclusters).ShouldNot(ContainElement(vdb.Spec.Subclusters[3].Name))
		Expect(subclusters).Should(ContainElement(vdb.Spec.Subclusters[4].Name))
	})
})

func getSubclusters(scMap map[string]string) []string {
	subclusters := []string{}
	for scName := range scMap {
		subclusters = append(subclusters, scName)
	}
	return subclusters
}
