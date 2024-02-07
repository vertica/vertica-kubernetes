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

package vrpqstatus

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gtypes "github.com/onsi/gomega/types"
	"github.com/vertica/vcluster/vclusterops"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"

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

	err = vapi.AddToScheme(scheme.Scheme)
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

	RunSpecs(t, "vrpqstatus Suite")
}

func EqualVerticaRestorePointsQueryCondition(expected interface{}) gtypes.GomegaMatcher {
	return &representVerticaRestorePointsQueryCondition{
		expected: expected,
	}
}

type representVerticaRestorePointsQueryCondition struct {
	expected interface{}
}

func (matcher *representVerticaRestorePointsQueryCondition) Match(actual interface{}) (success bool, err error) {
	response, ok := actual.(metav1.Condition)
	if !ok {
		return false, fmt.Errorf("representVerticaRestorePointsQueryCondition matcher expects a vapi.VerticaRestorePointsQueryCondition")
	}
	expectedObj, ok := matcher.expected.(metav1.Condition)
	if !ok {
		return false, fmt.Errorf("representVerticaRestorePointsQueryCondition should compare with a metav1.Condition")
	}
	// Compare everything except lastTransitionTime
	return response.Type == expectedObj.Type && response.Status == expectedObj.Status, nil
}

func (matcher *representVerticaRestorePointsQueryCondition) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%#v\nto equal\n\t%#v", actual, matcher.expected)
}

func (matcher *representVerticaRestorePointsQueryCondition) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%#v\nto not equal\n\t%#v", actual, matcher.expected)
}

var _ = Describe("status", func() {
	ctx := context.Background()

	It("should update status condition when no conditions have been set", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		cond := []metav1.Condition{
			{Type: vapi.Querying, Status: metav1.ConditionTrue, Reason: v1.UnknownReason},
		}
		Expect(Update(ctx, k8sClient, logger, vrpq, []*metav1.Condition{&cond[0]}, "", nil)).Should(Succeed())
		fetchVdb := &vapi.VerticaRestorePointsQuery{}
		nm := types.NamespacedName{Namespace: vrpq.Namespace, Name: vrpq.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaRestorePointsQuery{vrpq, fetchVdb} {
			Expect(len(v.Status.Conditions)).Should(Equal(1))
			Expect(v.Status.Conditions[0]).Should(EqualVerticaRestorePointsQueryCondition(cond[0]))
		}
	})

	It("should be able to change an existing status condition", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		conds := []metav1.Condition{
			{Type: vapi.Querying, Status: metav1.ConditionTrue, Reason: v1.UnknownReason},
			{Type: vapi.Querying, Status: metav1.ConditionFalse, Reason: v1.UnknownReason},
		}
		for i := range conds {
			Expect(Update(ctx, k8sClient, logger, vrpq, []*metav1.Condition{&conds[i]}, "", nil)).Should(Succeed())
			fetchVdb := &vapi.VerticaRestorePointsQuery{}
			nm := types.NamespacedName{Namespace: vrpq.Namespace, Name: vrpq.Name}
			Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
			for _, v := range []*vapi.VerticaRestorePointsQuery{vrpq, fetchVdb} {
				Expect(len(v.Status.Conditions)).Should(Equal(1))
				Expect(v.Status.Conditions[0]).Should(EqualVerticaRestorePointsQueryCondition(conds[i]))
			}
		}
	})

	It("should be able to handle multiple status conditions", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		conds := []metav1.Condition{
			{Type: vapi.Querying, Status: metav1.ConditionTrue, Reason: v1.UnknownReason},
			{Type: vapi.Querying, Status: metav1.ConditionFalse, Reason: v1.UnknownReason},
			{Type: vapi.QueryComplete, Status: metav1.ConditionTrue, Reason: v1.UnknownReason},
		}

		for i := range conds {
			Expect(Update(ctx, k8sClient, logger, vrpq, []*metav1.Condition{&conds[i]}, "", nil)).Should(Succeed())
		}

		fetchVdb := &vapi.VerticaRestorePointsQuery{}
		nm := types.NamespacedName{Namespace: vrpq.Namespace, Name: vrpq.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaRestorePointsQuery{vrpq, fetchVdb} {
			Expect(len(v.Status.Conditions)).Should(Equal(2))
			Expect(v.Status.Conditions[0]).Should(EqualVerticaRestorePointsQueryCondition(conds[1]))
			Expect(v.Status.Conditions[1]).Should(EqualVerticaRestorePointsQueryCondition(conds[2]))
		}
	})

	It("should change the lastTransitionTime when a status condition is changed", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		origTime := metav1.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
		Expect(Update(ctx, k8sClient, logger, vrpq,
			[]*metav1.Condition{{Type: vapi.Querying, Status: metav1.ConditionFalse, LastTransitionTime: origTime,
				Reason: v1.UnknownReason}}, "", nil)).Should(Succeed())
		Expect(Update(ctx, k8sClient, logger, vrpq,
			[]*metav1.Condition{{Type: vapi.Querying, Status: metav1.ConditionTrue, LastTransitionTime: origTime,
				Reason: v1.UnknownReason}}, "", nil)).Should(Succeed())
		Expect(vrpq.Status.Conditions[0].LastTransitionTime).ShouldNot(Equal(origTime))
	})

	It("should update the message status", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		msg := "Querying"
		msg1 := "Query successful"
		cond := v1.MakeCondition(vapi.Querying, metav1.ConditionTrue, "")

		Expect(Update(ctx, k8sClient, logger, vrpq,
			[]*metav1.Condition{cond}, msg, nil)).Should(Succeed())

		nm := types.NamespacedName{Namespace: vrpq.Namespace, Name: vrpq.Name}
		Expect(k8sClient.Get(ctx, nm, vrpq)).Should(Succeed())
		Expect(vrpq.Status.State).Should(Equal(msg))

		Expect(Update(ctx, k8sClient, logger, vrpq,
			[]*metav1.Condition{cond}, msg1, nil)).Should(Succeed())
		Expect(k8sClient.Get(ctx, nm, vrpq)).Should(Succeed())
		Expect(vrpq.Status.State).Should(Equal(msg1))
	})

	It("should update the restore points status", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		cond := v1.MakeCondition(vapi.Querying, metav1.ConditionTrue, "")
		restorePoints := []vclusterops.RestorePoint{
			{Archive: "db", Timestamp: "2024-02-06 07:25:07.437957", ID: "1465516c-e207-4d33-ae62-ce7cd5cfe8d0", Index: 1},
		}

		Expect(Update(ctx, k8sClient, logger, vrpq,
			[]*metav1.Condition{cond}, "", restorePoints)).Should(Succeed())

		nm := types.NamespacedName{Namespace: vrpq.Namespace, Name: vrpq.Name}
		Expect(k8sClient.Get(ctx, nm, vrpq)).Should(Succeed())
		Expect(vrpq.Status.RestorePoints[0].Archive).Should(Equal(restorePoints[0].Archive))
		Expect(vrpq.Status.RestorePoints[0].Timestamp).Should(Equal(restorePoints[0].Timestamp))
	})
})
