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

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"net/http"
	_ "net/http/pprof" //nolint:gosec

	"github.com/go-logr/zapr"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/et"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/vas"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	"github.com/vertica/vertica-kubernetes/pkg/security"
	//+kubebuilder:scaffold:imports
)

const (
	CertDir = "/tmp/k8s-webhook-server/serving-certs"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(vapi.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// getIsWebhookEnabled will return true if the webhook is enabled
func getIsWebhookEnabled() bool {
	const DefaultEnabled = true
	const EnableWebhookEnv = "ENABLE_WEBHOOKS"
	enableWebhook, found := os.LookupEnv(EnableWebhookEnv)
	if !found {
		return DefaultEnabled
	}
	enabled, err := strconv.ParseBool(enableWebhook)
	setupLog.Info(fmt.Sprintf("Parsed %s env var", enableWebhook),
		"value", enableWebhook, "enabled", enabled, "err", err)
	if err != nil {
		return DefaultEnabled
	}
	return enabled
}

// addReconcilersToManager will add a controller for each CR that this operator
// handles.  If any failure occurs, if will exit the program.
func addReconcilersToManager(mgr manager.Manager, restCfg *rest.Config, oc *opcfg.OperatorConfig) {
	if err := (&vdb.VerticaDBReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("VerticaDB"),
		Scheme: mgr.GetScheme(),
		Cfg:    restCfg,
		EVRec:  mgr.GetEventRecorderFor(vmeta.OperatorName),
		OpCfg:  *oc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VerticaDB")
		os.Exit(1)
	}

	if err := (&vas.VerticaAutoscalerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		EVRec:  mgr.GetEventRecorderFor(vmeta.OperatorName),
		Log:    ctrl.Log.WithName("controllers").WithName("VerticaAutoscaler"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VerticaAutoscaler")
		os.Exit(1)
	}
	if err := (&et.EventTriggerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("EventTrigger"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EventTrigger")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder
}

// addWebhooktsToManager will add any webhooks to the manager.  If any failure
// occurs, it will exit the program.
func addWebhooksToManager(mgr manager.Manager) {
	// Set the minimum TLS version for the webhook.  By default it will use
	// TLS 1.0, which has a lot of security flaws.  This is a hacky way to
	// set this and should be removed once there is a supported way.
	// There are numerous proposals to allow this to be configured from
	// Manager -- based on most recent activity this one looks promising:
	// https://github.com/kubernetes-sigs/controller-runtime/issues/852
	webhookServer := mgr.GetWebhookServer()
	webhookServer.TLSMinVersion = "1.3"

	if err := (&vapi.VerticaDB{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "VerticaDB")
		os.Exit(1)
	}
	if err := (&vapi.VerticaAutoscaler{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "VerticaAutoscaler")
		os.Exit(1)
	}
	if err := (&vapi.EventTrigger{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "EventTrigger")
		os.Exit(1)
	}
}

// setupWebhook will setup the webhook in the manager if enabled
func setupWebhook(ctx context.Context, mgr manager.Manager, restCfg *rest.Config, oc *opcfg.OperatorConfig) error {
	if getIsWebhookEnabled() {
		ns, err := getOperatorNamespace()
		if err != nil {
			return fmt.Errorf("failed to setup the webhook: %w", err)
		}
		if oc.WebhookCertSecret == "" {
			if err := security.GenerateWebhookCert(ctx, &setupLog, restCfg, CertDir, oc.PrefixName, ns); err != nil {
				return err
			}
		} else if !oc.SkipWebhookPatch {
			if err := security.PatchWebhookCABundleFromSecret(ctx, &setupLog, restCfg, oc.WebhookCertSecret,
				oc.PrefixName, ns); err != nil {
				return err
			}
		}
		addWebhooksToManager(mgr)
	}
	return nil
}

// getOperatorNamespace retrieves the namespace that the operator is running in
func getOperatorNamespace() (string, error) {
	const namespaceEnvVar = "OPERATOR_NAMESPACE"
	ns, found := os.LookupEnv(namespaceEnvVar)
	if !found {
		return "", fmt.Errorf("the environment variable %s must be set", namespaceEnvVar)
	}
	return ns, nil
}

// getReadinessProbeCallack returns the check to use for the readiness probe
func getReadinessProbeCallback(mgr ctrl.Manager) healthz.Checker {
	// If the webhook is enabled, we use a checker that tests if the webhook is
	// able to accept requests.
	if getIsWebhookEnabled() {
		return mgr.GetWebhookServer().StartedChecker()
	}
	return healthz.Ping
}

func main() {
	oc := &opcfg.OperatorConfig{}
	oc.SetFlagArgs()
	flag.Parse()

	logger := oc.GetLogger()
	if oc.FilePath != "" {
		log.Printf("Now logging in file %s", oc.FilePath)
	}

	ctrl.SetLogger(zapr.NewLogger(logger))

	if oc.EnableProfiler {
		go func() {
			server := &http.Server{
				Addr:              "localhost:6060",
				ReadHeaderTimeout: 3 * time.Second,
			}
			setupLog.Info("Opening profiling port", "addr", server.Addr)
			if err := server.ListenAndServe(); err != nil {
				setupLog.Error(err, "Profiling port closed")
			}
		}()
	}

	restCfg := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     oc.MetricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: oc.ProbeAddr,
		LeaderElection:         oc.EnableLeaderElection,
		LeaderElectionID:       "5c1e6227.vertica.com",
		Namespace:              "", // Empty namespace means watch all namespaces
		CertDir:                CertDir,
		Controller: v1alpha1.ControllerConfigurationSpec{
			GroupKindConcurrency: map[string]int{
				vapi.GkVDB.String(): oc.VerticaDBConcurrency,
				vapi.GkVAS.String(): oc.VerticaAutoscalerConcurrency,
				vapi.GkET.String():  oc.EventTriggerConcurrency,
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	addReconcilersToManager(mgr, restCfg, oc)
	ctx := ctrl.SetupSignalHandler()
	if err := setupWebhook(ctx, mgr, restCfg, oc); err != nil {
		setupLog.Error(err, "unable to setup webhook")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", getReadinessProbeCallback(mgr)); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
