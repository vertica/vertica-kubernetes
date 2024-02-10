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

package vscrstatus

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var k8sClient client.Client
var testEnv *envtest.Environment
var logger logr.Logger

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
	restCfg := cfg

	err = v1beta1.AddToScheme(scheme.Scheme)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "vscrstatus Suite")
}

var _ = Describe("status", func() {
	ctx := context.Background()

	It("should update status condition when no conditions have been set", func() {
		vscr := v1beta1.MakeVscr()
		Expect(k8sClient.Create(ctx, vscr)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vscr)).Should(Succeed()) }()

		cond := vapi.MakeCondition(v1beta1.ScrutinizePodCreated, metav1.ConditionTrue, "")
		Expect(UpdateCondition(ctx, k8sClient, vscr, cond)).Should(Succeed())
		fetchVscr := &v1beta1.VerticaScrutinize{}
		Expect(k8sClient.Get(ctx, vscr.ExtractNamespacedName(), fetchVscr)).Should(Succeed())
		for _, v := range []*v1beta1.VerticaScrutinize{vscr, fetchVscr} {
			Expect(len(v.Status.Conditions)).Should(Equal(1))
			Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(*cond))
		}
	})

	It("should be able to change an existing status condition", func() {
		vscr := v1beta1.MakeVscr()
		Expect(k8sClient.Create(ctx, vscr)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vscr)).Should(Succeed()) }()

		conds := []metav1.Condition{
			{Type: v1beta1.ScrutinizePodCreated, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
			{Type: v1beta1.ScrutinizePodCreated, Status: metav1.ConditionFalse, Reason: vapi.UnknownReason},
		}

		for i := range conds {
			Expect(UpdateCondition(ctx, k8sClient, vscr, &conds[i])).Should(Succeed())
			fetchVscr := &v1beta1.VerticaScrutinize{}
			Expect(k8sClient.Get(ctx, vscr.ExtractNamespacedName(), fetchVscr)).Should(Succeed())
			for _, v := range []*v1beta1.VerticaScrutinize{vscr, fetchVscr} {
				Expect(len(v.Status.Conditions)).Should(Equal(1))
				Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(conds[i]))
			}
		}
	})

	It("should be able to handle multiple status conditions", func() {
		vscr := v1beta1.MakeVscr()
		Expect(k8sClient.Create(ctx, vscr)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vscr)).Should(Succeed()) }()

		stat := &v1beta1.VerticaScrutinizeStatus{}
		stat.Conditions = []metav1.Condition{
			{Type: v1beta1.ScrutinizePodCreated, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
			{Type: v1beta1.ScrutinizeCollectionFinished, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
		}
		Expect(UpdateStatus(ctx, k8sClient, vscr, stat)).Should(Succeed())

		fetchVscr := &v1beta1.VerticaScrutinize{}
		nm := types.NamespacedName{Namespace: vscr.Namespace, Name: vscr.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVscr)).Should(Succeed())
		for _, v := range []*v1beta1.VerticaScrutinize{vscr, fetchVscr} {
			Expect(len(v.Status.Conditions)).Should(Equal(2))
			Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(stat.Conditions[0]))
			Expect(v.Status.Conditions[1]).Should(test.EqualMetaV1Condition(stat.Conditions[1]))
		}
	})

	It("should change the lastTransitionTime when a status condition is changed", func() {
		vscr := v1beta1.MakeVscr()
		Expect(k8sClient.Create(ctx, vscr)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vscr)).Should(Succeed()) }()

		origTime := metav1.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
		Expect(UpdateCondition(ctx, k8sClient, vscr,
			&metav1.Condition{Type: v1beta1.ScrutinizePodCreated, Status: metav1.ConditionFalse, LastTransitionTime: origTime,
				Reason: vapi.UnknownReason})).Should(Succeed())
		Expect(UpdateCondition(ctx, k8sClient, vscr,
			&metav1.Condition{Type: v1beta1.ScrutinizePodCreated, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason})).Should(Succeed())
		Expect(vscr.Status.Conditions[0].LastTransitionTime).ShouldNot(Equal(origTime))
	})

	It("should return false in IsStatusConditionTrue if condition isn't present", func() {
		vscr := v1beta1.MakeVscr()
		Expect(k8sClient.Create(ctx, vscr)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vscr)).Should(Succeed()) }()

		Expect(vscr.IsStatusConditionTrue(v1beta1.ScrutinizePodCreated)).Should(BeFalse())
		Expect(UpdateCondition(ctx, k8sClient, vscr,
			&metav1.Condition{Type: v1beta1.ScrutinizePodCreated, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason})).Should(Succeed())
		Expect(vscr.IsStatusConditionTrue(v1beta1.ScrutinizePodCreated)).Should(BeTrue())
	})

	It("should update all status fields", func() {
		vscr := v1beta1.MakeVscr()
		Expect(k8sClient.Create(ctx, vscr)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vscr)).Should(Succeed()) }()

		const podName = "pod1"
		const podUID = types.UID("pod1-uid")
		const tarballName = "pod1.tar"
		stat := &v1beta1.VerticaScrutinizeStatus{
			PodName:     podName,
			PodUID:      podUID,
			TarballName: tarballName,
		}
		stat.Conditions = []metav1.Condition{
			{Type: v1beta1.ScrutinizePodCreated, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
			{Type: v1beta1.ScrutinizeCollectionFinished, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
		}
		Expect(UpdateStatus(ctx, k8sClient, vscr, stat)).Should(Succeed())

		fetchVscr := &v1beta1.VerticaScrutinize{}
		nm := types.NamespacedName{Namespace: vscr.Namespace, Name: vscr.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVscr)).Should(Succeed())
		for _, v := range []*v1beta1.VerticaScrutinize{vscr, fetchVscr} {
			Expect(len(v.Status.Conditions)).Should(Equal(2))
			Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(stat.Conditions[0]))
			Expect(v.Status.Conditions[1]).Should(test.EqualMetaV1Condition(stat.Conditions[1]))
			Expect(v.Status.PodName).Should(Equal(podName))
			Expect(v.Status.PodUID).Should(Equal(podUID))
			Expect(v.Status.TarballName).Should(Equal(tarballName))
		}

	})
})
