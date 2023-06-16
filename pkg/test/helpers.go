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

package test

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega" //nolint:revive,stylecheck
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodRunningState bool

const (
	AllPodsRunning    PodRunningState = true
	AllPodsNotRunning PodRunningState = false
)

func CreatePods(ctx context.Context, c client.Client, vdb *vapi.VerticaDB, podRunningState PodRunningState) {
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		const ExpectOffset = 2
		CreateSts(ctx, c, vdb, sc, ExpectOffset, int32(i), podRunningState)
	}
}

func CreateStorageClass(ctx context.Context, c client.Client, allowVolumeExpansion bool) {
	stoclass := builder.BuildStorageClass(allowVolumeExpansion)
	Expect(c.Create(ctx, stoclass)).Should(Succeed())
}

func CreateSts(ctx context.Context, c client.Client, vdb *vapi.VerticaDB, sc *vapi.Subcluster, offset int,
	scIndex int32, podRunningState PodRunningState) {
	sts := &appsv1.StatefulSet{}
	if err := c.Get(ctx, names.GenStsName(vdb, sc), sts); kerrors.IsNotFound(err) {
		sts = builder.BuildStsSpec(names.GenStsName(vdb, sc), vdb, sc, builder.DefaultDeploymentNames())
		ExpectWithOffset(offset, c.Create(ctx, sts)).Should(Succeed())
	}
	for j := int32(0); j < sc.Size; j++ {
		pod := &corev1.Pod{}
		if err := c.Get(ctx, names.GenPodName(vdb, sc, j), pod); kerrors.IsNotFound(err) {
			pod = builder.BuildPod(vdb, sc, j)
			ExpectWithOffset(offset, c.Create(ctx, pod)).Should(Succeed())
			setPodStatusHelper(ctx, c, offset+1, names.GenPodName(vdb, sc, j), scIndex, j, podRunningState, false)
		}
		pv := &corev1.PersistentVolume{}
		if err := c.Get(ctx, names.GenPVName(vdb, sc, j), pv); kerrors.IsNotFound(err) {
			pv := builder.BuildPV(vdb, sc, j)
			ExpectWithOffset(offset, c.Create(ctx, pv)).Should(Succeed())
		}
		pvc := &corev1.PersistentVolumeClaim{}
		if err := c.Get(ctx, names.GenPVCName(vdb, sc, j), pvc); kerrors.IsNotFound(err) {
			pvc := builder.BuildPVC(vdb, sc, j)
			ExpectWithOffset(offset, c.Create(ctx, pvc)).Should(Succeed())
			pvc.Status.Phase = corev1.ClaimBound
			ExpectWithOffset(offset, c.Status().Update(ctx, pvc)).Should(Succeed())
		}
	}
	// Update the status in the sts to reflect the number of pods we created
	sts.Status.Replicas = sc.Size
	sts.Status.ReadyReplicas = sc.Size
	ExpectWithOffset(offset, c.Status().Update(ctx, sts))
}

func ScaleDownSubcluster(ctx context.Context, c client.Client, vdb *vapi.VerticaDB, sc *vapi.Subcluster, newSize int32) {
	ExpectWithOffset(1, sc.Size).Should(BeNumerically(">=", newSize))
	for i := newSize; i < sc.Size; i++ {
		pod := &corev1.Pod{}
		ExpectWithOffset(1, c.Get(ctx, names.GenPodName(vdb, sc, i), pod)).Should(Succeed())
		ExpectWithOffset(1, c.Delete(ctx, pod)).Should(Succeed())
	}

	// Update the status field of the sts
	sts := &appsv1.StatefulSet{}
	ExpectWithOffset(1, c.Get(ctx, names.GenStsName(vdb, sc), sts)).Should(Succeed())
	sts.Status.Replicas = newSize
	sts.Status.ReadyReplicas = newSize
	ExpectWithOffset(1, c.Status().Update(ctx, sts))

	// Update the subcluster size
	sc.Size = newSize
	ExpectWithOffset(1, c.Update(ctx, vdb)).Should(Succeed())
	for i := range vdb.Status.Subclusters {
		scs := &vdb.Status.Subclusters[i]
		if scs.Name == sc.Name {
			scs.InstallCount = newSize
			scs.AddedToDBCount = newSize
			break
		}
	}
	ExpectWithOffset(1, c.Status().Update(ctx, vdb)).Should(Succeed())
}

func FakeIPv6ForPod(scIndex, podIndex int32) string {
	return fmt.Sprintf("fdf8:f535:82e4::%x", scIndex*100+podIndex)
}

func FakeIPForPod(scIndex, podIndex int32) string {
	return fmt.Sprintf("192.168.%d.%d", scIndex, podIndex)
}

func setPodStatusHelper(ctx context.Context, c client.Client, funcOffset int, podName types.NamespacedName,
	scIndex, podIndex int32, podRunningState PodRunningState, ipv6 bool) {
	pod := &corev1.Pod{}
	ExpectWithOffset(funcOffset, c.Get(ctx, podName, pod)).Should(Succeed())

	// Since we using a fake kubernetes cluster, none of the pods we
	// create will actually be changed to run. Some testcases depend
	// on that, so we will update the pod status to show that they
	// are running.
	if podRunningState {
		pod.Status.Phase = corev1.PodRunning
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Ready: true}}
	}
	// We assign a fake IP that is deterministic so that it is easily
	// identifiable in a test.
	if ipv6 {
		pod.Status.PodIP = FakeIPv6ForPod(scIndex, podIndex)
	} else {
		pod.Status.PodIP = FakeIPForPod(scIndex, podIndex)
	}

	ExpectWithOffset(funcOffset, c.Status().Update(ctx, pod))
	if podRunningState {
		ExpectWithOffset(funcOffset, c.Get(ctx, podName, pod)).Should(Succeed())
		ExpectWithOffset(funcOffset, pod.Status.Phase).Should(Equal(corev1.PodRunning))
	}
}

func SetPodStatus(ctx context.Context, c client.Client, funcOffset int, podName types.NamespacedName,
	scIndex, podIndex int32, podRunningState PodRunningState) {
	setPodStatusHelper(ctx, c, funcOffset, podName, scIndex, podIndex, podRunningState, false)
}

func DeletePods(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		const ExpectOffset = 2
		DeleteSts(ctx, c, vdb, sc, ExpectOffset)
	}
}

func DeleteStorageClass(ctx context.Context, c client.Client) {
	stoclass := &storagev1.StorageClass{}
	err := c.Get(ctx, types.NamespacedName{Name: builder.TestStorageClassName}, stoclass)
	if !kerrors.IsNotFound(err) {
		Expect(c.Delete(ctx, stoclass)).Should(Succeed())
	}
}

func DeleteSecret(ctx context.Context, c client.Client, name string) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Name: name}, secret)
	if !kerrors.IsNotFound(err) {
		Expect(c.Delete(ctx, secret))
	}
}

func DeleteSts(ctx context.Context, c client.Client, vdb *vapi.VerticaDB, sc *vapi.Subcluster, offset int) {
	for j := int32(0); j < sc.Size; j++ {
		pod := &corev1.Pod{}
		err := c.Get(ctx, names.GenPodName(vdb, sc, j), pod)
		if !kerrors.IsNotFound(err) {
			ExpectWithOffset(offset, c.Delete(ctx, pod)).Should(Succeed())
		}
		pvc := &corev1.PersistentVolumeClaim{}
		err = c.Get(ctx, names.GenPVCName(vdb, sc, j), pvc)
		if !kerrors.IsNotFound(err) {
			// Clear the finalizer to allow us to delete the PVC
			pvc.Finalizers = nil
			ExpectWithOffset(1, c.Update(ctx, pvc)).Should(Succeed())
			ExpectWithOffset(offset, c.Delete(ctx, pvc)).Should(Succeed())
			err = c.Get(ctx, names.GenPVCName(vdb, sc, j), pvc)
			ExpectWithOffset(1, err).ShouldNot(Succeed())
		}
		pv := &corev1.PersistentVolume{}
		err = c.Get(ctx, names.GenPVName(vdb, sc, j), pv)
		if !kerrors.IsNotFound(err) {
			ExpectWithOffset(offset, c.Delete(ctx, pv)).Should(Succeed())
		}
	}
	sts := &appsv1.StatefulSet{}
	err := c.Get(ctx, names.GenStsName(vdb, sc), sts)
	if !kerrors.IsNotFound(err) {
		ExpectWithOffset(offset, c.Delete(ctx, sts)).Should(Succeed())
	}
}

func CreateSvcs(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	svc := builder.BuildHlSvc(names.GenHlSvcName(vdb), vdb)
	ExpectWithOffset(1, c.Create(ctx, svc)).Should(Succeed())
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		svc := builder.BuildExtSvc(names.GenExtSvcName(vdb, sc), vdb, sc, builder.MakeSvcSelectorLabelsForServiceNameRouting)
		ExpectWithOffset(1, c.Create(ctx, svc)).Should(Succeed())
	}
}

func DeleteSvcs(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		svc := &corev1.Service{}
		err := c.Get(ctx, names.GenExtSvcName(vdb, sc), svc)
		if !kerrors.IsNotFound(err) {
			ExpectWithOffset(1, c.Delete(ctx, svc)).Should(Succeed())
		}
	}
	svc := &corev1.Service{}
	err := c.Get(ctx, names.GenHlSvcName(vdb), svc)
	if !kerrors.IsNotFound(err) {
		ExpectWithOffset(1, c.Delete(ctx, svc)).Should(Succeed())
	}
}

func CreateVAS(ctx context.Context, c client.Client, vas *vapi.VerticaAutoscaler) {
	ExpectWithOffset(1, c.Create(ctx, vas)).Should(Succeed())
}

func DeleteVAS(ctx context.Context, c client.Client, vas *vapi.VerticaAutoscaler) {
	ExpectWithOffset(1, c.Delete(ctx, vas)).Should(Succeed())
}

func CreateVDB(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	ExpectWithOffset(1, c.Create(ctx, vdb)).Should(Succeed())
}

func DeleteVDB(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	ExpectWithOffset(1, c.Delete(ctx, vdb)).Should(Succeed())
}
