/*
Copyright [2021-2024] Open Text.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vrpq

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cache"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var vrpqRec *VerticaRestorePointsQueryReconciler
var logger logr.Logger

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "VerticaRestorePointsQuery Suite")
}

var _ = BeforeSuite(func() {
	logger = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	logf.SetLogger(logger)

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = v1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = v1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	metricsServerOptions := metricsserver.Options{
		BindAddress: "0", // Disable metrics for the test
	}
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsServerOptions,
	})

	vrpqRec = &VerticaRestorePointsQueryReconciler{
		Client: k8sClient,
		Scheme: scheme.Scheme,
		Cfg:    cfg,
		Log:    logger,
		EVRec:  mgr.GetEventRecorderFor(vmeta.OperatorName),
	}
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

const testAccessKey = "dummy"
const testSecretKey = "dummy"

func createS3CredSecret(ctx context.Context, vdb *v1.VerticaDB) {
	createK8sCredSecret(ctx, vdb)
}

func createK8sCredSecret(ctx context.Context, vdb *v1.VerticaDB) {
	secret := builder.BuildCommunalCredSecret(vdb, testAccessKey, testSecretKey)
	Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
}

func createAzureAccountKeyCredSecret(ctx context.Context, vdb *v1.VerticaDB) {
	secret := builder.BuildAzureAccountKeyCommunalCredSecret(vdb, "verticaAccountName", "secretKey")
	Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
}

func createAzureSASCredSecret(ctx context.Context, vdb *v1.VerticaDB) {
	secret := builder.BuildAzureSASCommunalCredSecret(vdb, "blob.microsoft.net", "sharedAccessKey")
	Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
}

func deleteCommunalCredSecret(ctx context.Context, vdb *v1.VerticaDB) {
	deleteSecret(ctx, vdb, vdb.Spec.Communal.CredentialSecret)
}

func deleteSecret(ctx context.Context, vdb *v1.VerticaDB, secretName string) {
	nm := names.GenNamespacedName(vdb, secretName)
	secret := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, nm, secret)).Should(Succeed())
	Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
}

// mockVClusterOpsDispatchWithCustomSetup is like mockVClusterOpsDispatcher,
// except you provide your own setup API function.
func mockVClusterOpsDispatcherWithCustomSetup(vdb *v1.VerticaDB,
	setupAPIFunc func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger)) *vadmin.VClusterOps {
	evWriter := aterrors.TestEVWriter{}
	cacheManager := cache.MakeCacheManager(true)
	dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, "pwd", &evWriter, setupAPIFunc, cacheManager)
	vclusterops := dispatcher.(*vadmin.VClusterOps)
	fetcher := &cloud.SecretFetcher{
		Client:   vclusterops.Client,
		Log:      vclusterops.Log,
		Obj:      vclusterops.VDB,
		EVWriter: vclusterops.EVWriter,
	}
	cacheManager.InitCertCacheForVdb(vdb, fetcher)
	return vclusterops
}
