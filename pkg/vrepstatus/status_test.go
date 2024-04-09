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

package vrepstatus

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"

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

	RunSpecs(t, "vrepstatus Suite")
}

var _ = Describe("status", func() {
	ctx := context.Background()

	It("should update status condition when no conditions have been set", func() {
		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		cond := []metav1.Condition{
			{Type: v1beta1.ReplicationReady, Status: metav1.ConditionTrue, Reason: v1.UnknownReason},
		}
		Expect(Update(ctx, k8sClient, logger, vrep, []*metav1.Condition{&cond[0]}, "")).Should(Succeed())
		fetchVrep := &v1beta1.VerticaReplicator{}
		nm := types.NamespacedName{Namespace: vrep.Namespace, Name: vrep.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVrep)).Should(Succeed())
		for _, v := range []*v1beta1.VerticaReplicator{vrep, fetchVrep} {
			Expect(len(v.Status.Conditions)).Should(Equal(1))
			Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(cond[0]))
		}
	})

	It("should be able to change an existing status condition", func() {
		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		conds := []metav1.Condition{
			{Type: v1beta1.ReplicationReady, Status: metav1.ConditionTrue, Reason: v1.UnknownReason},
			{Type: v1beta1.ReplicationReady, Status: metav1.ConditionFalse, Reason: v1.UnknownReason},
		}
		for i := range conds {
			Expect(Update(ctx, k8sClient, logger, vrep, []*metav1.Condition{&conds[i]}, "")).Should(Succeed())
			fetchVrep := &v1beta1.VerticaReplicator{}
			nm := types.NamespacedName{Namespace: vrep.Namespace, Name: vrep.Name}
			Expect(k8sClient.Get(ctx, nm, fetchVrep)).Should(Succeed())
			for _, v := range []*v1beta1.VerticaReplicator{vrep, fetchVrep} {
				Expect(len(v.Status.Conditions)).Should(Equal(1))
				Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(conds[i]))
			}
		}
	})

	It("should be able to handle multiple status conditions", func() {
		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		conds := []metav1.Condition{
			{Type: v1beta1.ReplicationReady, Status: metav1.ConditionTrue, Reason: v1.UnknownReason},
			{Type: v1beta1.ReplicationReady, Status: metav1.ConditionFalse, Reason: v1.UnknownReason},
			{Type: v1beta1.ReplicationComplete, Status: metav1.ConditionTrue, Reason: v1.UnknownReason},
		}

		for i := range conds {
			Expect(Update(ctx, k8sClient, logger, vrep, []*metav1.Condition{&conds[i]}, "")).Should(Succeed())
		}

		fetchVrep := &v1beta1.VerticaReplicator{}
		nm := types.NamespacedName{Namespace: vrep.Namespace, Name: vrep.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVrep)).Should(Succeed())
		for _, v := range []*v1beta1.VerticaReplicator{vrep, fetchVrep} {
			Expect(len(v.Status.Conditions)).Should(Equal(2))
			Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(conds[1]))
			Expect(v.Status.Conditions[1]).Should(test.EqualMetaV1Condition(conds[2]))
		}
	})

	It("should change the lastTransitionTime when a status condition is changed", func() {
		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()
		origTime := metav1.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
		Expect(Update(ctx, k8sClient, logger, vrep,
			[]*metav1.Condition{{Type: v1beta1.ReplicationReady, Status: metav1.ConditionFalse, LastTransitionTime: origTime,
				Reason: v1.UnknownReason}}, "")).Should(Succeed())
		Expect(Update(ctx, k8sClient, logger, vrep,
			[]*metav1.Condition{{Type: v1beta1.ReplicationReady, Status: metav1.ConditionTrue, LastTransitionTime: origTime,
				Reason: v1.UnknownReason}}, "")).Should(Succeed())
		Expect(vrep.Status.Conditions[0].LastTransitionTime).ShouldNot(Equal(origTime))
	})

	It("should update the message status", func() {
		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()
		msg := "Replicating"
		msg1 := "Replication successful"
		cond := v1.MakeCondition(v1beta1.ReplicationReady, metav1.ConditionTrue, "")

		Expect(Update(ctx, k8sClient, logger, vrep,
			[]*metav1.Condition{cond}, msg)).Should(Succeed())

		nm := types.NamespacedName{Namespace: vrep.Namespace, Name: vrep.Name}
		Expect(k8sClient.Get(ctx, nm, vrep)).Should(Succeed())
		Expect(vrep.Status.State).Should(Equal(msg))

		Expect(Update(ctx, k8sClient, logger, vrep,
			[]*metav1.Condition{cond}, msg1)).Should(Succeed())
		Expect(k8sClient.Get(ctx, nm, vrep)).Should(Succeed())
		Expect(vrep.Status.State).Should(Equal(msg1))
	})
})
