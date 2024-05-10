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

package iter

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
)

var _ = Describe("sc_finder", func() {
	ctx := context.Background()

	It("should find all subclusters that exist in the vdb", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2", "sc3"}
		scSizes := []int32{10, 5, 8}
		const sbName = "sand"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
			{Name: scNames[2], Size: scSizes[2]},
		}

		verifySubclusters(ctx, vdb, scNames, scSizes, vapi.MainCluster, FindInVdb)
		// FindSubclusters should return an empty slice if a sandbox name,
		// that does not exist in the vdb status, is passed in
		verifySubclusters(ctx, vdb, []string{}, []int32{}, sbName, FindInVdb)

		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: scNames[:2]},
		}
		// FindSubclusters should only return subclusters that belong to the given
		// sandbox
		verifySubclusters(ctx, vdb, scNames[:2], scSizes[:2], sbName, FindInVdb)
		// FindSubclusters should only return subclusters that are not part of
		// any sandboxes
		verifySubclusters(ctx, vdb, scNames[2:], scSizes[2:], vapi.MainCluster, FindInVdb)
	})

	It("should find all subclusters regardless of cluster", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2", "sc3"}
		scSizes := []int32{10, 5, 8}
		const sbName = "sand"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
			{Name: scNames[2], Size: scSizes[2]},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: scNames[:2]},
		}
		verifySubclusters(ctx, vdb, scNames, scSizes, sbName, FindAllClusters)
	})

	It("should find subclusters that don't exist in the vdb", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		scSizes := []int32{10, 5}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		// We create a second vdb without one of the subclusters.  We then use
		// the finder to discover this additional subcluster.
		lookupVdb := vapi.MakeVDB()
		lookupVdb.Spec.Subclusters[0] = vapi.Subcluster{Name: scNames[0], Size: scSizes[0]}

		finder := MakeSubclusterFinder(k8sClient, lookupVdb)
		scs, err := finder.FindSubclusters(ctx, FindAll, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(len(scs)).Should(Equal(len(scNames)))
		for i := 0; i < len(scNames); i++ {
			Expect(scs[i].Name).Should(Equal(scNames[i]))
		}
	})

	It("should find subclusters if the name has been overridden", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		stsNames := []string{"first", "second"}
		scSizes := []int32{10, 5}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0], Annotations: map[string]string{vmeta.StsNameOverrideAnnotation: stsNames[0]}},
			{Name: scNames[1], Size: scSizes[1], Annotations: map[string]string{vmeta.StsNameOverrideAnnotation: stsNames[1]}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		finder := MakeSubclusterFinder(k8sClient, vdb)
		scs, err := finder.FindSubclusters(ctx, FindExisting, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(len(scs)).Should(Equal(len(scNames)))
		for i := 0; i < len(scNames); i++ {
			Expect(scs[i].Name).Should(Equal(scNames[i]))
			Expect(scs[i].Annotations).Should(HaveKeyWithValue(vmeta.StsNameOverrideAnnotation, stsNames[i]))
		}
	})

	It("should find statefulsets that don't exist in the vdb", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		scSizes := []int32{10, 5}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		// We create a second vdb without one of the subclusters.  We then use
		// the finder to discover this additional subcluster.
		lookupVdb := vapi.MakeVDB()
		lookupVdb.Spec.Subclusters[0] = vapi.Subcluster{Name: scNames[1], Size: scSizes[1]}

		finder := MakeSubclusterFinder(k8sClient, lookupVdb)
		sts, err := finder.FindStatefulSets(ctx, FindNotInVdb, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(len(sts.Items)).Should(Equal(1))
		Expect(sts.Items[0].Name).Should(Equal(names.GenStsName(vdb, &vdb.Spec.Subclusters[0]).Name))
	})

	It("should only find statefulsets and subclusters that exist in k8s", func() {
		vdb := vapi.MakeVDB()
		const RunningImage = "the-running-image"
		vdb.Spec.Image = RunningImage
		scNames := []string{"first", "second"}
		scSizes := []int32{2, 3}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0], Type: vapi.PrimarySubcluster},
			{Name: scNames[1], Size: scSizes[1], Type: vapi.SecondarySubcluster},
		}
		const sbName = "sand"
		// sandbox scNames[0]
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sbName, Subclusters: []vapi.SubclusterName{{Name: scNames[0]}}},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{scNames[0]}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		vdbCopy := *vdb // Make a copy for cleanup since we will mutate vdb
		defer test.DeletePods(ctx, k8sClient, &vdbCopy)
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)

		// Add another subcluster, but since we didn't create any k8s objects
		// for it, it won't be returned by the finder.
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, vapi.Subcluster{Name: "not there"})
		// Change the image in the vdb spec to prove that we fill in the image
		// from the statefulset
		vdb.Spec.Image = "should-not-report-this-image"
		finder := MakeSubclusterFinder(k8sClient, vdb)
		sts, err := finder.FindStatefulSets(ctx, FindExisting, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(len(sts.Items)).Should(Equal(1))
		Expect(sts.Items[0].Name).Should(Equal(names.GenStsName(vdb, &vdb.Spec.Subclusters[1]).Name))

		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: scNames[:1]},
		}

		scs, err := finder.FindSubclusters(ctx, FindExisting, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(len(scs)).Should(Equal(1))
		Expect(scs[0].Name).Should(Equal(vdb.Spec.Subclusters[1].Name))

		// only the sandboxed sts should be returned
		finder = MakeSubclusterFinder(k8sClient, vdb)
		sts, err = finder.FindStatefulSets(ctx, FindExisting, sbName)
		Expect(err).Should(Succeed())
		Expect(len(sts.Items)).Should(Equal(1))
		Expect(sts.Items[0].Name).Should(Equal(names.GenStsName(vdb, &vdb.Spec.Subclusters[0]).Name))
	})

	It("should find all pods that exist in k8s for the VerticaDB", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"a", "b"}
		scSizes := []int32{2, 3}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
		}
		const sbName = "sand"
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sbName, Subclusters: []vapi.SubclusterName{{Name: scNames[0]}}},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{scNames[0]}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		// When use the finder, pass in a Vdb that is entirely different then
		// the one we used above.  It will be ignored anyway when using
		// FindExisting.
		findPods(ctx, vapi.MakeVDB(), int(scSizes[1]), vapi.MainCluster)
		// Only the pods belonging to the sandboxed subcluster
		// will be collected
		findPods(ctx, vdb, int(scSizes[0]), sbName)
	})

	It("should find service objects that exist in the vdb", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)

		finder := MakeSubclusterFinder(k8sClient, vdb)
		svcs, err := finder.FindServices(ctx, FindInVdb, vapi.MainCluster)
		Expect(err).Should(Succeed())
		const SvcsPerSubcluster = 1
		Expect(len(svcs.Items)).Should(Equal(SvcsPerSubcluster))
		svcNames := []string{
			svcs.Items[0].Name,
		}
		Expect(svcNames).Should(ContainElements(
			names.GenExtSvcName(vdb, sc).Name,
		))
	})

	It("should find service objects that do not exist in the vdb", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0]},
			{Name: scNames[1]},
		}
		sc2 := &vdb.Spec.Subclusters[1]
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)

		// Use a different vdb for the finder so that we can find the service
		// objects missing from it.
		lookupVdb := vapi.MakeVDB()
		lookupVdb.Spec.Subclusters[0] = vapi.Subcluster{Name: scNames[0]}

		finder := MakeSubclusterFinder(k8sClient, lookupVdb)
		svcs, err := finder.FindServices(ctx, FindNotInVdb, vapi.MainCluster)
		Expect(err).Should(Succeed())
		const SvcsPerSubcluster = 1
		Expect(len(svcs.Items)).Should(Equal(SvcsPerSubcluster))
		svcNames := []string{
			svcs.Items[0].Name,
		}
		Expect(svcNames).Should(ContainElements(
			names.GenExtSvcName(vdb, sc2).Name,
		))
	})

	It("should return sorted svc if requested", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"zzend", "aafirst"}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0]},
			{Name: scNames[1]},
		}
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)

		finder := MakeSubclusterFinder(k8sClient, vdb)
		svcs, err := finder.FindServices(ctx, FindExisting|FindSorted, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(svcs.Items[0].Name).Should(ContainSubstring(scNames[1]))
		Expect(svcs.Items[1].Name).Should(ContainSubstring(scNames[0]))
	})

	It("should return sorted sts if requested", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"zzlast", "aaseemefirst"}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: 2},
			{Name: scNames[1], Size: 1},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		finder := MakeSubclusterFinder(k8sClient, vdb)
		stss, err := finder.FindStatefulSets(ctx, FindExisting|FindSorted, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(stss.Items[0].Name).Should(ContainSubstring(scNames[1]))
		Expect(stss.Items[1].Name).Should(ContainSubstring(scNames[0]))

		pods, err := finder.FindPods(ctx, FindExisting|FindSorted, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(pods.Items[0].Name).Should(ContainSubstring(scNames[1]))
		Expect(pods.Items[1].Name).Should(ContainSubstring(fmt.Sprintf("%s-0", scNames[0])))
		Expect(pods.Items[2].Name).Should(ContainSubstring(fmt.Sprintf("%s-1", scNames[0])))
	})

	It("should return sorted subclusters if requested", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"zzlast", "aaseemefirst"}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: 2},
			{Name: scNames[1], Size: 1},
		}

		finder := MakeSubclusterFinder(k8sClient, vdb)
		subclusters, err := finder.FindSubclusters(ctx, FindInVdb|FindSorted, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(len(subclusters)).Should(Equal(2))
		Expect(subclusters[0].Name).Should(ContainSubstring(scNames[1]))
		Expect(subclusters[1].Name).Should(ContainSubstring(scNames[0]))
	})
})

func verifySubclusters(ctx context.Context, vdb *vapi.VerticaDB, scNames []string,
	scSizes []int32, sandbox string, flag FindFlags) {
	finder := MakeSubclusterFinder(k8sClient, vdb)
	scs, err := finder.FindSubclusters(ctx, flag, sandbox)
	Expect(err).Should(Succeed())
	Expect(len(scs)).Should(Equal(len(scNames)))
	for i := 0; i < len(scNames); i++ {
		Expect(scs[i].Name).Should(Equal(scNames[i]))
		Expect(scs[i].Size).Should(Equal(scSizes[i]))
	}
}

func findPods(ctx context.Context, vdb *vapi.VerticaDB, size int,
	sandbox string) {
	finder := MakeSubclusterFinder(k8sClient, vdb)
	pods, err := finder.FindPods(ctx, FindExisting, sandbox)
	Expect(err).Should(Succeed())
	Expect(len(pods.Items)).Should(Equal(size))
}
