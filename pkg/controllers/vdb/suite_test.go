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

package vdb

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var k8sClient client.Client
var testEnv *envtest.Environment
var logger logr.Logger
var restCfg *rest.Config
var vdbRec *VerticaDBReconciler

var _ = BeforeSuite(func() {
	logger = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	logf.SetLogger(logger)

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, cfg).NotTo(BeNil())
	restCfg = cfg

	err = vapi.AddToScheme(scheme.Scheme)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	err = v1beta1.AddToScheme(scheme.Scheme)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	metricsServerOptions := metricsserver.Options{
		BindAddress: "0", // Disable metrics for the test
	}
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsServerOptions,
	})
	Expect(err).NotTo(HaveOccurred())

	vdbRec = &VerticaDBReconciler{
		Client: k8sClient,
		Log:    logger,
		Scheme: scheme.Scheme,
		Cfg:    restCfg,
		EVRec:  mgr.GetEventRecorderFor(vmeta.OperatorName),
	}
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "vdb Suite")
}

func setVerticaNodeNameInPodFacts(vdb *vapi.VerticaDB, sc *vapi.Subcluster, pf *podfacts.PodFacts) {
	for podIndex := int32(0); podIndex < sc.Size; podIndex++ {
		podNm := names.GenPodName(vdb, sc, podIndex)
		pf.Detail[podNm].SetVnodeName(fmt.Sprintf("v_%s_node%04d", strings.ToLower(vdb.Spec.DBName), podIndex+1))
		pf.Detail[podNm].SetCompat21NodeName(fmt.Sprintf("node%04d", podIndex+1))
	}
}

func defaultPodFactOverrider(_ context.Context, _ *vapi.VerticaDB, pf *podfacts.PodFact, _ *podfacts.GatherState) error {
	if !pf.GetIsPodRunning() {
		return nil
	}
	pf.SetEulaAccepted(true)
	pf.SetIsInstalled(true)
	pf.SetDirExists(map[string]bool{
		paths.ConfigLogrotatePath: true,
		paths.ConfigSharePath:     true,
	})
	pf.SetDBExists(true)
	pf.SetStartupInProgress(false)
	pf.SetUpNode(true)
	pf.SetSubclusterOid("123456")
	pf.SetShardSubscriptions(1)
	return nil
}

// createPodFactsDefault will generate the PodFacts for test using the default settings for all.
func createPodFactsDefault(fpr *cmds.FakePodRunner) *podfacts.PodFacts {
	pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
	pfacts.OverrideFunc = defaultPodFactOverrider
	return &pfacts
}

func createPodFactsWithNoDB(ctx context.Context, vdb *vapi.VerticaDB, fpr *cmds.FakePodRunner, numPodsToChange int) *podfacts.PodFacts {
	pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
	// Change a number of pods to indicate db doesn't exist.  Due to the map that
	// stores the pod facts, the specific pods we change are non-deterministic.
	podsChanged := 0
	pfacts.OverrideFunc = func(ctx context.Context, vdb *vapi.VerticaDB, pf *podfacts.PodFact, gs *podfacts.GatherState) error {
		if err := defaultPodFactOverrider(ctx, vdb, pf, gs); err != nil {
			return err
		}
		if podsChanged == numPodsToChange {
			return nil
		}
		pf.SetDBExists(false)
		pf.SetUpNode(false)
		podsChanged++
		return nil
	}
	ExpectWithOffset(1, pfacts.Collect(ctx, vdb)).Should(Succeed())
	return &pfacts
}

func createPodFactsWithInstallNeeded(ctx context.Context, vdb *vapi.VerticaDB, fpr *cmds.FakePodRunner) *podfacts.PodFacts {
	pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
	pfacts.OverrideFunc = func(ctx context.Context, vdb *vapi.VerticaDB, pfact *podfacts.PodFact, gs *podfacts.GatherState) error {
		pfact.SetIsPodRunning(true)
		pfact.SetIsInstalled(false)
		pfact.SetDBExists(false)
		pfact.SetEulaAccepted(false)
		pfact.SetUpNode(false)
		return nil
	}
	ExpectWithOffset(1, pfacts.Collect(ctx, vdb)).Should(Succeed())
	return &pfacts
}

func createPodFactsWithRestartNeeded(ctx context.Context, vdb *vapi.VerticaDB, sc *vapi.Subcluster,
	fpr *cmds.FakePodRunner, podsDownByIndex []int32, readOnly bool) *podfacts.PodFacts {
	pfacts := createPodFactsDefault(fpr)
	ExpectWithOffset(1, pfacts.Collect(ctx, vdb)).Should(Succeed())
	for _, podIndex := range podsDownByIndex {
		downPodNm := names.GenPodName(vdb, sc, podIndex)
		// If readOnly is true, pod will be up and running.
		pfacts.Detail[downPodNm].SetUpNode(readOnly)
		pfacts.Detail[downPodNm].SetReadOnly(readOnly)
	}
	return pfacts
}

func createPodFactsWithSlowStartup(ctx context.Context, vdb *vapi.VerticaDB, sc *vapi.Subcluster,
	fpr *cmds.FakePodRunner, slowPodsByIndex []int32) *podfacts.PodFacts {
	pfacts := createPodFactsDefault(fpr)
	ExpectWithOffset(1, pfacts.Collect(ctx, vdb)).Should(Succeed())
	for _, podIndex := range slowPodsByIndex {
		downPodNm := names.GenPodName(vdb, sc, podIndex)
		pfacts.Detail[downPodNm].SetStartupInProgress(true)
		pfacts.Detail[downPodNm].SetUpNode(false)
	}
	return pfacts
}

const testAccessKey = "dummy"
const testSecretKey = "dummy"
const testClientKey = "dummy"

func createS3CredSecret(ctx context.Context, vdb *vapi.VerticaDB) {
	createK8sCredSecret(ctx, vdb)
}

func createK8sCredSecret(ctx context.Context, vdb *vapi.VerticaDB) {
	secret := builder.BuildCommunalCredSecret(vdb, testAccessKey, testSecretKey)
	Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
}

func createAzureAccountKeyCredSecret(ctx context.Context, vdb *vapi.VerticaDB) {
	secret := builder.BuildAzureAccountKeyCommunalCredSecret(vdb, "verticaAccountName", "secretKey")
	Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
}

func createAzureSASCredSecret(ctx context.Context, vdb *vapi.VerticaDB) {
	secret := builder.BuildAzureSASCommunalCredSecret(vdb, "blob.microsoft.net", "sharedAccessKey")
	Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
}

func createS3SseCustomerKeySecret(ctx context.Context, vdb *vapi.VerticaDB) {
	secret := builder.BuildS3SseCustomerKeySecret(vdb, testClientKey)
	Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
}

func deleteCommunalCredSecret(ctx context.Context, vdb *vapi.VerticaDB) {
	deleteSecret(ctx, vdb, vdb.Spec.Communal.CredentialSecret)
}

func deleteS3SseCustomerKeySecret(ctx context.Context, vdb *vapi.VerticaDB) {
	deleteSecret(ctx, vdb, vdb.Spec.Communal.S3SseCustomerKeySecret)
}

func deleteSecret(ctx context.Context, vdb *vapi.VerticaDB, secretName string) {
	nm := names.GenNamespacedName(vdb, secretName)
	secret := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, nm, secret)).Should(Succeed())
	Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
}

func deleteConfigMap(ctx context.Context, vdb *vapi.VerticaDB, cmName string) {
	nm := names.GenNamespacedName(vdb, cmName)
	cm := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, nm, cm)).Should(Succeed())
	Expect(k8sClient.Delete(ctx, cm)).Should(Succeed())
}

const (
	maincluster = "main"
	subcluster1 = "sc1"
)
