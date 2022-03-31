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

package vasstatus

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
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
		"vasstatus Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = Describe("status", func() {
	ctx := context.Background()

	It("should set the selector in the status field", func() {
		vas := vapi.MakeVAS()
		vas.Spec.SubclusterServiceName = "my-svc"
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		expectedSelector := getLabelSelector(vas)

		nm := vapi.MakeVASName()
		req := ctrl.Request{NamespacedName: nm}
		Expect(SetSelector(ctx, k8sClient, logger, &req)).Should(Succeed())

		fetchVas := &vapi.VerticaAutoscaler{}
		Expect(k8sClient.Get(ctx, nm, fetchVas)).Should(Succeed())
		Expect(fetchVas.Status.Selector).Should(Equal(expectedSelector))
	})

	It("should increment the scaling count", func() {
		vas := vapi.MakeVAS()
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		nm := vapi.MakeVASName()
		req := ctrl.Request{NamespacedName: nm}
		Expect(IncrScalingCount(ctx, k8sClient, logger, &req)).Should(Succeed())

		Expect(k8sClient.Get(ctx, nm, vas)).Should(Succeed())
		Expect(vas.Status.ScalingCount).Should(Equal(1))

		Expect(IncrScalingCount(ctx, k8sClient, logger, &req)).Should(Succeed())

		Expect(k8sClient.Get(ctx, nm, vas)).Should(Succeed())
		Expect(vas.Status.ScalingCount).Should(Equal(2))
	})

	It("should tolerate a non-existent vas", func() {
		nm := vapi.MakeVASName()
		req := ctrl.Request{NamespacedName: nm}
		Expect(IncrScalingCount(ctx, k8sClient, logger, &req)).Should(Succeed())
		Expect(SetSelector(ctx, k8sClient, logger, &req)).Should(Succeed())
	})
})
