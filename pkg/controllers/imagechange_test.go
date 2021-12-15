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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("imagechange", func() {
	ctx := context.Background()

	It("should correctly pick the image change type", func() {
		vdb := vapi.MakeVDB()

		vdb.Spec.ImageChangePolicy = vapi.OfflineImageChange
		Expect(onlineImageChangeAllowed(vdb)).Should(Equal(false))
		Expect(offlineImageChangeAllowed(vdb)).Should(Equal(true))
		vdb.Spec.ImageChangePolicy = vapi.OnlineImageChange
		Expect(onlineImageChangeAllowed(vdb)).Should(Equal(true))
		Expect(offlineImageChangeAllowed(vdb)).Should(Equal(false))

		// No version -- offline
		vdb.Spec.ImageChangePolicy = vapi.AutoImageChange
		vdb.Spec.LicenseSecret = "license"
		vdb.Spec.KSafety = vapi.KSafety1
		delete(vdb.ObjectMeta.Annotations, vapi.VersionAnnotation)
		Expect(offlineImageChangeAllowed(vdb)).Should(Equal(true))

		// Older version
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = "v11.0.0"
		Expect(offlineImageChangeAllowed(vdb)).Should(Equal(true))

		// Correct version
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = version.OnlineImageChangeVersion
		Expect(onlineImageChangeAllowed(vdb)).Should(Equal(true))

		// Missing license
		vdb.Spec.LicenseSecret = ""
		Expect(offlineImageChangeAllowed(vdb)).Should(Equal(true))
		vdb.Spec.LicenseSecret = "license-again"

		// k-safety 0
		vdb.Spec.KSafety = vapi.KSafety0
		Expect(offlineImageChangeAllowed(vdb)).Should(Equal(true))
	})

	It("should not need an image change if images match in sts and vdb", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		mgr := MakeImageChangeManager(vrec, logger, vdb, func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.IsImageChangeNeeded(ctx)).Should(Equal(false))
	})

	It("should change the image of just the primaries or just secondaries", func() {
		const OldImage = "old-image"
		const NewImage1 = "new-image-1"
		const NewImage2 = "new-image-2"
		vdb := vapi.MakeVDB()
		vdb.Spec.Image = OldImage
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 2, IsPrimary: true},
			{Name: "sc2", Size: 3, IsPrimary: false},
		}
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)
		vdb.Spec.Image = NewImage1
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)

		mgr := MakeImageChangeManager(vrec, logger, vdb, func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.IsImageChangeNeeded(ctx)).Should(Equal(true))
		stsChange, res, err := mgr.updateImageInStatefulSets(ctx, true, false)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(stsChange).Should(Equal(1))

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image).Should(Equal(NewImage1))
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image).Should(Equal(OldImage))

		vdb.Spec.Image = NewImage2
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		stsChange, res, err = mgr.updateImageInStatefulSets(ctx, false, true)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(stsChange).Should(Equal(1))

		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image).Should(Equal(NewImage1))
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image).Should(Equal(NewImage2))
	})
})
