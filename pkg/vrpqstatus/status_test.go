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
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
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

type representVerticaRestorePointsQueryCondition struct {
	expected interface{}
}

func EqualVerticaRestorePointsQueryCondition(expected interface{}) gtypes.GomegaMatcher {
	return &representVerticaRestorePointsQueryCondition{
		expected: expected,
	}
}

func (matcher *representVerticaRestorePointsQueryCondition) Match(actual interface{}) (success bool, err error) {
	response, ok := actual.(vapi.VerticaRestorePointsQueryCondition)
	if !ok {
		return false, fmt.Errorf("representVerticaRestorePointsQueryCondition matcher expects a vapi.VerticaRestorePointsQueryCondition")
	}
	expectedObj, ok := matcher.expected.(vapi.VerticaRestorePointsQueryCondition)
	if !ok {
		return false, fmt.Errorf("representVerticaRestorePointsQueryCondition should compare with a vapi.VerticaRestorePointsQueryCondition")
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

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "vrpqstatus Suite")
}

var _ = Describe("status", func() {
	ctx := context.Background()

	It("should update status condition when no conditions have been set", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		cond := vapi.VerticaRestorePointsQueryCondition{Type: vapi.Querying, Status: corev1.ConditionTrue}
		Expect(UpdateCondition(ctx, k8sClient, logger, vrpq, cond)).Should(Succeed())
		fetchVdb := &vapi.VerticaRestorePointsQuery{}
		nm := types.NamespacedName{Namespace: vrpq.Namespace, Name: vrpq.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaRestorePointsQuery{vrpq, fetchVdb} {
			Expect(len(v.Status.Conditions)).Should(Equal(1))
			Expect(v.Status.Conditions[0]).Should(EqualVerticaRestorePointsQueryCondition(cond))
		}
	})

	It("should be able to change an existing status condition", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		conds := []vapi.VerticaRestorePointsQueryCondition{
			{Type: vapi.Querying, Status: corev1.ConditionTrue},
			{Type: vapi.Querying, Status: corev1.ConditionFalse},
		}
		for _, cond := range conds {
			Expect(UpdateCondition(ctx, k8sClient, logger, vrpq, cond)).Should(Succeed())
			fetchVdb := &vapi.VerticaRestorePointsQuery{}
			nm := types.NamespacedName{Namespace: vrpq.Namespace, Name: vrpq.Name}
			Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
			for _, v := range []*vapi.VerticaRestorePointsQuery{vrpq, fetchVdb} {
				Expect(len(v.Status.Conditions)).Should(Equal(1))
				Expect(v.Status.Conditions[0]).Should(EqualVerticaRestorePointsQueryCondition(cond))
			}
		}
	})

	It("should be able to handle multiple status conditions", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		conds := []vapi.VerticaRestorePointsQueryCondition{
			{Type: vapi.Querying, Status: corev1.ConditionTrue},
			{Type: vapi.Querying, Status: corev1.ConditionFalse},
			{Type: vapi.QueryComplete, Status: corev1.ConditionTrue},
		}
		for _, cond := range conds {
			Expect(UpdateCondition(ctx, k8sClient, logger, vrpq, cond)).Should(Succeed())
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
		Expect(UpdateCondition(ctx, k8sClient, logger, vrpq,
			vapi.VerticaRestorePointsQueryCondition{Type: vapi.Querying, Status: corev1.ConditionFalse, LastTransitionTime: origTime},
		)).Should(Succeed())
		Expect(UpdateCondition(ctx, k8sClient, logger, vrpq,
			vapi.VerticaRestorePointsQueryCondition{Type: vapi.Querying, Status: corev1.ConditionTrue},
		)).Should(Succeed())
		Expect(vrpq.Status.Conditions[0].LastTransitionTime).ShouldNot(Equal(origTime))
	})

})
