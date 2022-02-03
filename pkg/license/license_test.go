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

package license

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
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
		"license Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var TestLicenseSecretName = types.NamespacedName{Namespace: "default", Name: "license"}

func makeLicenseSecret(licenseKeys []string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestLicenseSecretName.Name,
			Namespace: TestLicenseSecretName.Namespace,
		},
		StringData: map[string]string{},
	}

	// Populate license files with dummy data.  We aren't validating the data in
	// the tests.  Only that a secret can have multiple licenses.
	for _, key := range licenseKeys {
		secret.StringData[key] = "<dummy>"
	}

	return secret
}

func createLicenseSecret(ctx context.Context, secret *corev1.Secret) {
	ExpectWithOffset(1, k8sClient.Create(ctx, secret)).Should(Succeed())
}

func deleteLicenseSecret(ctx context.Context, secret *corev1.Secret) {
	ExpectWithOffset(1, k8sClient.Delete(ctx, secret)).Should(Succeed())
}

var _ = Describe("license", func() {
	ctx := context.Background()

	It("should return CE license if license secret is missing", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.LicenseSecret = ""

		Expect(GetPath(ctx, k8sClient, vdb)).Should(Equal(paths.CELicensePath))
	})

	It("should return custom CE license if license secret is set", func() {
		LicenseName := "licenseA"
		secret := makeLicenseSecret([]string{LicenseName})
		createLicenseSecret(ctx, secret)
		defer deleteLicenseSecret(ctx, secret)

		vdb := vapi.MakeVDB()
		vdb.Spec.LicenseSecret = TestLicenseSecretName.Name
		expectedPath := fmt.Sprintf("%s/%s", paths.MountedLicensePath, LicenseName)
		Expect(GetPath(ctx, k8sClient, vdb)).Should(Equal(expectedPath))
	})

	It("should return license that is alphabetically first if secret has multiple licenses", func() {
		LicenseNames := []string{"lic1001", "lic8992", "lic1000"}
		FirstLicense := LicenseNames[2]
		secret := makeLicenseSecret(LicenseNames)
		createLicenseSecret(ctx, secret)
		defer deleteLicenseSecret(ctx, secret)

		vdb := vapi.MakeVDB()
		vdb.Spec.LicenseSecret = TestLicenseSecretName.Name
		expectedPath := fmt.Sprintf("%s/%s", paths.MountedLicensePath, FirstLicense)
		Expect(GetPath(ctx, k8sClient, vdb)).Should(Equal(expectedPath))
	})

	It("should return CE license if secret exists but is empty", func() {
		secret := makeLicenseSecret([]string{})
		createLicenseSecret(ctx, secret)
		defer deleteLicenseSecret(ctx, secret)

		vdb := vapi.MakeVDB()
		vdb.Spec.LicenseSecret = TestLicenseSecretName.Name
		Expect(GetPath(ctx, k8sClient, vdb)).Should(Equal(paths.CELicensePath))
	})
})
