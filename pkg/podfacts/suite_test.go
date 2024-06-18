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

package podfacts

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var k8sClient client.Client
var testEnv *envtest.Environment
var logger logr.Logger
var restCfg *rest.Config

// VerticaDBReconciler reconciles a VerticaDB object
type VerticaDBReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Cfg    *rest.Config
	EVRec  record.EventRecorder
}

// GetSuperuserPassword returns the superuser password if it has been provided
func (r *VerticaDBReconciler) GetSuperuserPassword(ctx context.Context, log logr.Logger, vdb *vapi.VerticaDB) (string, error) {
	return vk8s.GetSuperuserPassword(ctx, r.Client, log, r, vdb)
}

// Event a wrapper for Event() that also writes a log entry
func (r *VerticaDBReconciler) Event(vdb runtime.Object, eventtype, reason, message string) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Event(vdb, eventtype, reason, message)
}

// Eventf is a wrapper for Eventf() that also writes a log entry
func (r *VerticaDBReconciler) Eventf(vdb runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Eventf(vdb, eventtype, reason, messageFmt, args...)
}

// GetClient gives access to the Kubernetes client
func (r *VerticaDBReconciler) GetClient() client.Client {
	return r.Client
}

// GetEventRecorder gives access to the event recorder
func (r *VerticaDBReconciler) GetEventRecorder() record.EventRecorder {
	return r.EVRec
}

// GetConfig gives access to *rest.Config
func (r *VerticaDBReconciler) GetConfig() *rest.Config {
	return r.Cfg
}

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

	k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0", // Disable metrics for the test
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

	RunSpecs(t, "security Suite")
}
