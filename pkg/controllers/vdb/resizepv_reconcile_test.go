/*
 (c) Copyright [2021-2022] Open Text.
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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("resizepv_reconcile", func() {
	ctx := context.Background()
	const NewPVSize = "55Gi"

	It("should resize PVC when requestSize changes", func() {
		vdb := vapi.MakeVDB()
		test.CreateStorageClass(ctx, k8sClient, true)
		defer test.DeleteStorageClass(ctx, k8sClient)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		resizeLocalStorage(ctx, vdb, NewPVSize)
		runResizePVReconciler(ctx, vdb, true, false)
		checkPVCSize(ctx, vdb, true)
		runResizePVReconciler(ctx, vdb, true, false)
		mockResizeStatusUpdate(ctx, vdb, NewPVSize)
		runResizePVReconciler(ctx, vdb, false, true)
	})

	It("should skip resize if provisioner doesn't allow volume expansion", func() {
		vdb := vapi.MakeVDB()
		test.CreateStorageClass(ctx, k8sClient, false)
		defer test.DeleteStorageClass(ctx, k8sClient)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		checkPVCSize(ctx, vdb, true)
		resizeLocalStorage(ctx, vdb, NewPVSize)
		runResizePVReconciler(ctx, vdb, false, false)
		// Verify that we didn't update the PVC because the storage class
		// doesn't allow volume expansion
		checkPVCSize(ctx, vdb, false)
	})

	It("should requeue if database isn't up", func() {
		vdb := vapi.MakeVDB()
		test.CreateStorageClass(ctx, k8sClient, true)
		defer test.DeleteStorageClass(ctx, k8sClient)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		resizeLocalStorage(ctx, vdb, NewPVSize)
		// Run reconciler to update the PVC
		runResizePVReconciler(ctx, vdb, true, false)
		checkPVCSize(ctx, vdb, true)
		mockResizeStatusUpdate(ctx, vdb, NewPVSize)
		// Run reconciler to update vertica.  This will requeue because database isn't up
		runResizePVReconciler(ctx, vdb, true, false)
	})
})

func resizeLocalStorage(ctx context.Context, vdb *vapi.VerticaDB, newSize string) {
	ExpectWithOffset(1, k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
	vdb.Spec.Local.RequestSize = resource.MustParse(newSize)
	ExpectWithOffset(1, k8sClient.Update(ctx, vdb)).Should(Succeed())
}

func runResizePVReconciler(ctx context.Context, vdb *vapi.VerticaDB, expectedRequeue, expectedDepotAlter bool) {
	fpr := &cmds.FakePodRunner{}
	pfacts := createPodFactsDefault(fpr)
	ExpectWithOffset(1, pfacts.Collect(ctx, vdb)).Should(Succeed())
	// Mock that depot size for each pod is 60%
	for i := range pfacts.Detail {
		pfacts.Detail[i].depotDiskPercentSize = "60%"
	}
	r := MakeResizePVReconciler(vdbRec, vdb, fpr, pfacts)
	res, err := r.Reconcile(ctx, &ctrl.Request{})
	ExpectWithOffset(1, err).Should(Succeed())
	ExpectWithOffset(1, res).Should(Equal(ctrl.Result{Requeue: expectedRequeue}))
	alterCommands := fpr.FindCommands("select alter_location_size")
	ExpectWithOffset(1, len(alterCommands) > 0).Should(Equal(expectedDepotAlter))
}

func checkPVCSize(ctx context.Context, vdb *vapi.VerticaDB, expectedMatch bool) {
	pvc := &corev1.PersistentVolumeClaim{}
	for s := range vdb.Spec.Subclusters {
		for i := int32(0); i < vdb.Spec.Subclusters[s].Size; i++ {
			ExpectWithOffset(1, k8sClient.Get(ctx, names.GenPVCName(vdb, &vdb.Spec.Subclusters[s], i), pvc)).Should(Succeed())
			ExpectWithOffset(1, pvc.Spec.Resources.Requests.Storage().Equal(vdb.Spec.Local.RequestSize)).Should(Equal(expectedMatch))
		}
	}
}

func mockResizeStatusUpdate(ctx context.Context, vdb *vapi.VerticaDB, newSize string) {
	pvc := &corev1.PersistentVolumeClaim{}
	for s := range vdb.Spec.Subclusters {
		for i := int32(0); i < vdb.Spec.Subclusters[s].Size; i++ {
			ExpectWithOffset(1, k8sClient.Get(ctx, names.GenPVCName(vdb, &vdb.Spec.Subclusters[s], i), pvc)).Should(Succeed())
			pvc.Status.Capacity = corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(newSize),
			}
			ExpectWithOffset(1, k8sClient.Status().Update(ctx, pvc)).Should(Succeed())
		}
	}
}
