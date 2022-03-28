/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package vdbstatus

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	gtypes "github.com/onsi/gomega/types"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
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
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"vdbstatus Suite",
		[]Reporter{printer.NewlineReporter{}})
}

// EqualVerticaDBCondition is a custom matcher to use for VerticaDBCondition
// that doesn't compare the LastTransitionTime
func EqualVerticaDBCondition(expected interface{}) gtypes.GomegaMatcher {
	return &representVerticaDBCondition{
		expected: expected,
	}
}

type representVerticaDBCondition struct {
	expected interface{}
}

func (matcher *representVerticaDBCondition) Match(actual interface{}) (success bool, err error) {
	response, ok := actual.(vapi.VerticaDBCondition)
	if !ok {
		return false, fmt.Errorf("RepresentVerticaDBCondition matcher expects a vapi.VerticaDBCondition")
	}

	expectedObj, ok := matcher.expected.(vapi.VerticaDBCondition)
	if !ok {
		return false, fmt.Errorf("RepresentVerticaDBCondition should compare with a vapi.VerticaDBCondition")
	}

	// Compare everything except lastTransitionTime
	return response.Type == expectedObj.Type && response.Status == expectedObj.Status, nil
}

func (matcher *representVerticaDBCondition) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%#v\nto equal\n\t%#v", actual, matcher.expected)
}

func (matcher *representVerticaDBCondition) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%#v\nto not equal\n\t%#v", actual, matcher.expected)
}

var _ = Describe("status", func() {
	ctx := context.Background()

	It("should update status condition when no conditions have been set", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		cond := vapi.VerticaDBCondition{Type: vapi.AutoRestartVertica, Status: corev1.ConditionTrue}
		Expect(UpdateCondition(ctx, k8sClient, vdb, cond)).Should(Succeed())
		fetchVdb := &vapi.VerticaDB{}
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
			Expect(len(v.Status.Conditions)).Should(Equal(1))
			Expect(v.Status.Conditions[0]).Should(EqualVerticaDBCondition(cond))
		}
	})

	It("should be able to change an existing status condition", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		conds := []vapi.VerticaDBCondition{
			{Type: vapi.AutoRestartVertica, Status: corev1.ConditionTrue},
			{Type: vapi.AutoRestartVertica, Status: corev1.ConditionFalse},
		}

		for _, cond := range conds {
			Expect(UpdateCondition(ctx, k8sClient, vdb, cond)).Should(Succeed())
			fetchVdb := &vapi.VerticaDB{}
			nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
			Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
			for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
				Expect(len(v.Status.Conditions)).Should(Equal(1))
				Expect(v.Status.Conditions[0]).Should(EqualVerticaDBCondition(cond))
			}
		}
	})

	It("should be able to handle multiple status conditions", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		conds := []vapi.VerticaDBCondition{
			{Type: vapi.DBInitialized, Status: corev1.ConditionTrue},
			{Type: vapi.AutoRestartVertica, Status: corev1.ConditionTrue},
			{Type: vapi.AutoRestartVertica, Status: corev1.ConditionFalse},
		}

		for _, cond := range conds {
			Expect(UpdateCondition(ctx, k8sClient, vdb, cond)).Should(Succeed())
		}

		fetchVdb := &vapi.VerticaDB{}
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
			Expect(len(v.Status.Conditions)).Should(Equal(2))
			Expect(v.Status.Conditions[0]).Should(EqualVerticaDBCondition(conds[2]))
			Expect(v.Status.Conditions[1]).Should(EqualVerticaDBCondition(conds[0]))
		}
	})

	It("should change the lastTransitionTime when a status condition is changed", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		origTime := metav1.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
		Expect(UpdateCondition(ctx, k8sClient, vdb,
			vapi.VerticaDBCondition{Type: vapi.AutoRestartVertica, Status: corev1.ConditionFalse, LastTransitionTime: origTime},
		)).Should(Succeed())
		Expect(UpdateCondition(ctx, k8sClient, vdb,
			vapi.VerticaDBCondition{Type: vapi.AutoRestartVertica, Status: corev1.ConditionTrue},
		)).Should(Succeed())
		Expect(vdb.Status.Conditions[0].LastTransitionTime).ShouldNot(Equal(origTime))
	})
})
