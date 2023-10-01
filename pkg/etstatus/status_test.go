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

package etstatus

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
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

	RunSpecs(t, "etstatus Suite")
}

var _ = Describe("status", func() {
	ctx := context.Background()

	It("should append a new reference object status when one isn't there already", func() {
		et := vapi.MakeET()
		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		refObj := mockETRefObjectStatus()
		refObj.JobNamespace = "default"
		refObj.JobName = "job1"
		Expect(Apply(ctx, k8sClient, logger, et, refObj)).Should(Succeed())
		verifyETRefObjectStatusInET(ctx, et.ExtractNamespacedName(), refObj, 0)
	})

	It("should update an existing reference object status when one isn't there already", func() {
		et := vapi.MakeET()
		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		refObj := mockETRefObjectStatus()
		Expect(Apply(ctx, k8sClient, logger, et, refObj)).Should(Succeed())
		verifyETRefObjectStatusInET(ctx, et.ExtractNamespacedName(), refObj, 0)

		// Now add the Job information.
		refObj.JobNamespace = refObj.Namespace
		refObj.JobName = "create-tables"
		Expect(Apply(ctx, k8sClient, logger, et, refObj)).Should(Succeed())
		verifyETRefObjectStatusInET(ctx, et.ExtractNamespacedName(), refObj, 0)
	})

	It("should allow for multiple reference object status if different objects", func() {
		et := vapi.MakeET()
		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		v1RefObj := mockETRefObjectStatus()
		Expect(Apply(ctx, k8sClient, logger, et, v1RefObj)).Should(Succeed())
		verifyETRefObjectStatusInET(ctx, et.ExtractNamespacedName(), v1RefObj, 0)

		v2RefObj := v1RefObj.DeepCopy()
		v2RefObj.Name = "v2"
		Expect(Apply(ctx, k8sClient, logger, et, v2RefObj)).Should(Succeed())
		verifyETRefObjectStatusInET(ctx, et.ExtractNamespacedName(), v1RefObj, 0)
		verifyETRefObjectStatusInET(ctx, et.ExtractNamespacedName(), v2RefObj, 1)
	})
})

// mockETRefObjectStatus will generate a mock of a ETRefObjectStatus. It
// leaves out any job information.
func mockETRefObjectStatus() *vapi.ETRefObjectStatus {
	return &vapi.ETRefObjectStatus{
		APIVersion:      vapi.GroupVersion.String(),
		Kind:            vapi.VerticaDBKind,
		Namespace:       "default",
		Name:            "v1",
		UID:             "abcdef",
		ResourceVersion: "1",
	}
}

func verifyETRefObjectStatusInET(ctx context.Context, etName types.NamespacedName,
	expectedRefStatus *vapi.ETRefObjectStatus, refIndex int) {
	fetchedET := &vapi.EventTrigger{}
	ExpectWithOffset(1, k8sClient.Get(ctx, etName, fetchedET)).Should(Succeed())
	ExpectWithOffset(1, len(fetchedET.Status.References)).Should(BeNumerically(">", refIndex))
	ExpectWithOffset(1, reflect.DeepEqual(fetchedET.Status.References[refIndex], *expectedRefStatus)).Should(
		BeTrue(),
		"RefObjectStatus differs\nFetched status: %+v\nExpected status: %+v",
		fetchedET.Status.References[refIndex],
		*expectedRefStatus)
}
