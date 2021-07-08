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
	"fmt"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"yunion.io/x/pkg/tristate"
)

var k8sClient client.Client
var testEnv *envtest.Environment
var logger logr.Logger
var restCfg *rest.Config
var vrec *VerticaDBReconciler

var _ = BeforeSuite(func() {
	logger = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	logf.SetLogger(logger)

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, cfg).NotTo(BeNil())
	restCfg = cfg

	err = vapi.AddToScheme(scheme.Scheme)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0", // Disable metrics for the test
	})
	Expect(err).NotTo(HaveOccurred())

	vrec = &VerticaDBReconciler{
		Client: k8sClient,
		Log:    logger,
		Scheme: scheme.Scheme,
		Cfg:    restCfg,
		EVRec:  mgr.GetEventRecorderFor(OperatorName),
	}
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"K8s Suite",
		[]Reporter{printer.NewlineReporter{}})
}

type PodRunningState bool

const (
	AllPodsRunning    PodRunningState = true
	AllPodsNotRunning PodRunningState = false
)

func createPodHelper(ctx context.Context, vdb *vapi.VerticaDB, podRunningState PodRunningState, ipv6 bool) {
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		sts := buildStsSpec(names.GenStsName(vdb, sc), vdb, sc)
		ExpectWithOffset(1, k8sClient.Create(ctx, sts)).Should(Succeed())
		for j := int32(0); j < sc.Size; j++ {
			pod := buildPod(vdb, sc, j)
			ExpectWithOffset(1, k8sClient.Create(ctx, pod)).Should(Succeed())
			setPodStatusHelper(ctx, 2 /* funcOffset */, names.GenPodName(vdb, sc, j), int32(i), j, podRunningState, ipv6)
		}
		// Update the status in the sts to reflect the number of pods we created
		sts.Status.Replicas = sc.Size
		sts.Status.ReadyReplicas = sc.Size
		ExpectWithOffset(1, k8sClient.Status().Update(ctx, sts))
	}
}

func createPods(ctx context.Context, vdb *vapi.VerticaDB, podRunningState PodRunningState) {
	createPodHelper(ctx, vdb, podRunningState, false)
}

func createIPv6Pods(ctx context.Context, vdb *vapi.VerticaDB, podRunningState PodRunningState) {
	createPodHelper(ctx, vdb, podRunningState, true)
}

func fakeIPv6ForPod(scIndex, podIndex int32) string {
	return fmt.Sprintf("fdf8:f535:82e4::%x", scIndex*100+podIndex)
}

func fakeIPForPod(scIndex, podIndex int32) string {
	return fmt.Sprintf("192.168.%d.%d", scIndex, podIndex)
}

func setPodStatusHelper(ctx context.Context, funcOffset int, podName types.NamespacedName,
	scIndex, podIndex int32, podRunningState PodRunningState, ipv6 bool) {
	pod := &corev1.Pod{}
	ExpectWithOffset(funcOffset, k8sClient.Get(ctx, podName, pod)).Should(Succeed())

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
		pod.Status.PodIP = fakeIPv6ForPod(scIndex, podIndex)
	} else {
		pod.Status.PodIP = fakeIPForPod(scIndex, podIndex)
	}

	ExpectWithOffset(funcOffset, k8sClient.Status().Update(ctx, pod))
	if podRunningState {
		ExpectWithOffset(funcOffset, k8sClient.Get(ctx, podName, pod)).Should(Succeed())
		ExpectWithOffset(funcOffset, pod.Status.Phase).Should(Equal(corev1.PodRunning))
	}
}

func setPodStatus(ctx context.Context, funcOffset int, podName types.NamespacedName,
	scIndex, podIndex int32, podRunningState PodRunningState) {
	setPodStatusHelper(ctx, funcOffset, podName, scIndex, podIndex, podRunningState, false)
}

func deletePods(ctx context.Context, vdb *vapi.VerticaDB) {
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		for j := int32(0); j < sc.Size; j++ {
			pod := &corev1.Pod{}
			err := k8sClient.Get(ctx, names.GenPodName(vdb, sc, j), pod)
			if !kerrors.IsNotFound(err) {
				ExpectWithOffset(1, k8sClient.Delete(ctx, pod)).Should(Succeed())
			}
		}
		sts := &appsv1.StatefulSet{}
		ExpectWithOffset(1, k8sClient.Get(ctx, names.GenStsName(vdb, sc), sts)).Should(Succeed())
		ExpectWithOffset(1, k8sClient.Delete(ctx, sts)).Should(Succeed())
	}
}

func createSvcs(ctx context.Context, vdb *vapi.VerticaDB) {
	svc := buildHlSvc(names.GenHlSvcName(vdb), vdb)
	ExpectWithOffset(1, k8sClient.Create(ctx, svc)).Should(Succeed())
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		svc := buildExtSvc(names.GenExtSvcName(vdb, sc), vdb, sc)
		ExpectWithOffset(1, k8sClient.Create(ctx, svc)).Should(Succeed())
	}
}

func deleteSvcs(ctx context.Context, vdb *vapi.VerticaDB) {
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		svc := &corev1.Service{}
		err := k8sClient.Get(ctx, names.GenExtSvcName(vdb, sc), svc)
		if !kerrors.IsNotFound(err) {
			ExpectWithOffset(1, k8sClient.Delete(ctx, svc)).Should(Succeed())
		}
	}
	svc := &corev1.Service{}
	err := k8sClient.Get(ctx, names.GenHlSvcName(vdb), svc)
	if !kerrors.IsNotFound(err) {
		ExpectWithOffset(1, k8sClient.Delete(ctx, svc)).Should(Succeed())
	}
}

func scaleDownSubcluster(ctx context.Context, vdb *vapi.VerticaDB, sc *vapi.Subcluster, newSize int32) {
	ExpectWithOffset(1, sc.Size).Should(BeNumerically(">=", newSize))
	for i := newSize; i < sc.Size; i++ {
		pod := &corev1.Pod{}
		ExpectWithOffset(1, k8sClient.Get(ctx, names.GenPodName(vdb, sc, i), pod)).Should(Succeed())
		ExpectWithOffset(1, k8sClient.Delete(ctx, pod)).Should(Succeed())
	}

	// Update the status field of the sts
	sts := &appsv1.StatefulSet{}
	ExpectWithOffset(1, k8sClient.Get(ctx, names.GenStsName(vdb, sc), sts)).Should(Succeed())
	sts.Status.Replicas = newSize
	sts.Status.ReadyReplicas = newSize
	ExpectWithOffset(1, k8sClient.Status().Update(ctx, sts))

	// Update the subcluster size
	sc.Size = newSize
	ExpectWithOffset(1, k8sClient.Update(ctx, vdb)).Should(Succeed())
}

func createVdb(ctx context.Context, vdb *vapi.VerticaDB) {
	ExpectWithOffset(1, k8sClient.Create(ctx, vdb)).Should(Succeed())
}

func deleteVdb(ctx context.Context, vdb *vapi.VerticaDB) {
	ExpectWithOffset(1, k8sClient.Delete(ctx, vdb)).Should(Succeed())
}

func setVerticaNodeNameInPodFacts(vdb *vapi.VerticaDB, sc *vapi.Subcluster, pf *PodFacts) {
	for podIndex := int32(0); podIndex < sc.Size; podIndex++ {
		podNm := names.GenPodName(vdb, sc, podIndex)
		pf.Detail[podNm].vnodeName = fmt.Sprintf("v_%s_node%04d", vdb.Spec.DBName, podIndex+1)
		pf.Detail[podNm].compat21NodeName = fmt.Sprintf("node%04d", podIndex+1)
	}
}

func createPodFactsWithNoDB(ctx context.Context, vdb *vapi.VerticaDB, fpr *cmds.FakePodRunner, numPodsToChange int) *PodFacts {
	pfacts := MakePodFacts(k8sClient, fpr)
	ExpectWithOffset(1, pfacts.Collect(ctx, vdb)).Should(Succeed())
	// Change a number of pods to indicate db doesn't exist.  Due to the map that
	// stores the pod facts, the specific pods we change are non-deterministic.
	podsChanged := 0
	for _, pfact := range pfacts.Detail {
		if podsChanged == numPodsToChange {
			break
		}
		pfact.dbExists = tristate.False
		pfact.upNode = false
		podsChanged++
	}
	return &pfacts
}

func createPodFactsWithRestartNeeded(ctx context.Context, vdb *vapi.VerticaDB, sc *vapi.Subcluster,
	fpr *cmds.FakePodRunner, podsDownByIndex []int32) *PodFacts {
	pfacts := MakePodFacts(k8sClient, fpr)
	ExpectWithOffset(1, pfacts.Collect(ctx, vdb)).Should(Succeed())
	for _, podIndex := range podsDownByIndex {
		downPodNm := names.GenPodName(vdb, sc, podIndex)
		pfacts.Detail[downPodNm].upNode = false
	}
	return &pfacts
}

const testAccessKey = "dummy"
const testSecretKey = "dummy"

func createCommunalCredSecret(ctx context.Context, vdb *vapi.VerticaDB) {
	secret := buildCommunalCredSecret(vdb, testAccessKey, testSecretKey)
	Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
}

func deleteCommunalCredSecret(ctx context.Context, vdb *vapi.VerticaDB) {
	nm := names.GenCommunalCredSecretName(vdb)
	secret := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, nm, secret)).Should(Succeed())
	Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
}
