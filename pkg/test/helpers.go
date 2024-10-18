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

package test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega" //nolint:stylecheck
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodRunningState bool

const (
	AllPodsRunning    PodRunningState = true
	AllPodsNotRunning PodRunningState = false
	TestKeyValue                      = "test-key"
	TestCertValue                     = "test-cert"
	TestCaCertValue                   = "test-ca-cert"
)

func CreatePods(ctx context.Context, c client.Client, vdb *vapi.VerticaDB, podRunningState PodRunningState) {
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		const ExpectOffset = 2
		CreateSts(ctx, c, vdb, sc, ExpectOffset, int32(i), podRunningState) //nolint:gosec
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
		sts = builder.BuildStsSpec(names.GenStsName(vdb, sc), vdb, sc)
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

func CreateConfigMap(ctx context.Context, c client.Client, vdb *vapi.VerticaDB, id, sbName string) {
	nm := names.GenSandboxConfigMapName(vdb, sbName)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				vmeta.SandboxControllerUpgradeTriggerID: id,
				vmeta.VersionAnnotation:                 "v23.4.0",
			},
			Name:      nm.Name,
			Namespace: vdb.Namespace,
		},
		Data: map[string]string{
			vapi.VerticaDBNameKey: vdb.Name,
			vapi.SandboxNameKey:   sbName,
		},
	}
	Expect(c.Create(ctx, cm)).Should(Succeed())
	Expect(cm.Annotations[vmeta.SandboxControllerUpgradeTriggerID]).Should(Equal(id))
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
			scs.AddedToDBCount = newSize
			scs.Detail = []vapi.VerticaDBPodStatus{}
			for j := int32(0); j < newSize; j++ {
				scs.Detail = append(scs.Detail, vapi.VerticaDBPodStatus{Installed: true})
			}
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

func SetPodContainerStatus(ctx context.Context, c client.Client, podName types.NamespacedName,
	cntStatuses []corev1.ContainerStatus) {
	pod := &corev1.Pod{}
	ExpectWithOffset(1, c.Get(ctx, podName, pod)).Should(Succeed())
	pod.Status.ContainerStatuses = cntStatuses
	ExpectWithOffset(1, c.Status().Update(ctx, pod))
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

func DeleteConfigMap(ctx context.Context, c client.Client, vdb *vapi.VerticaDB, sbName string) {
	cm := &corev1.ConfigMap{}
	nm := names.GenSandboxConfigMapName(vdb, sbName)
	err := c.Get(ctx, nm, cm)
	if !kerrors.IsNotFound(err) {
		Expect(c.Delete(ctx, cm)).Should(Succeed())
	}
}

func CreateFakeTLSSecret(ctx context.Context, vdb *vapi.VerticaDB, c client.Client, name string) {
	secret := BuildTLSSecret(vdb, name, TestKeyValue, TestCertValue, TestCaCertValue)
	Expect(c.Create(ctx, secret)).Should(Succeed())
}

func BuildTLSSecret(vdb *vapi.VerticaDB, name, key, cert, rootca string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: vdb.Namespace,
		},
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey:        []byte(key),
			corev1.TLSCertKey:              []byte(cert),
			corev1.ServiceAccountRootCAKey: []byte(rootca),
		},
	}
	return secret
}

func CreateSuperuserPasswordSecret(ctx context.Context, vdb *vapi.VerticaDB, c client.Client,
	name, password string) {
	secret := BuildSuperuserPasswordSecret(vdb, name, password)
	Expect(c.Create(ctx, secret)).Should(Succeed())
}

func BuildSuperuserPasswordSecret(vdb *vapi.VerticaDB, name, password string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: vdb.Namespace,
		},
		StringData: map[string]string{
			names.SuperuserPasswordKey: password,
		},
	}
	return secret
}

func DeleteSecret(ctx context.Context, c client.Client, name string) {
	nm := vapi.MakeVDBName() // The secret is expected to be created in the same namespace as the test standard vdb
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: nm.Namespace, Name: name}, secret)
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

func CreateVDB(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	ExpectWithOffset(1, c.Create(ctx, vdb)).Should(Succeed())
}

func DeleteVDB(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	ExpectWithOffset(1, c.Delete(ctx, vdb)).Should(Succeed())
}

func GetEventForObj(ctx context.Context, cl client.Client, obj client.Object) *corev1.EventList {
	// Even when using envtest, the events that were created aren't always
	// available right away. We keep trying to fetch the event.
	eventList := corev1.EventList{}
	evReader := func() int {
		Ω(cl.List(ctx, &eventList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("involvedObject.uid", string(obj.GetUID())),
		}))
		return len(eventList.Items)
	}
	const timeout = 10 * time.Second
	const pollInterval = 1 * time.Second
	Eventually(evReader).Within(timeout).ProbeEvery(pollInterval).Should(BeNumerically(">=", 1))
	return &eventList
}
