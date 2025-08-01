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

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"os"
	"time"

	// Allows us to pull in things generated from `go generate`
	_ "embed"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "net/http/pprof" //nolint:gosec

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	vapiV1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapiB1 "github.com/vertica/vertica-kubernetes/api/v1beta1"

	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	vcache "github.com/vertica/vertica-kubernetes/pkg/cache"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/et"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/sandbox"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/vas"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/vrep"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/vrpq"
	"github.com/vertica/vertica-kubernetes/pkg/controllers/vscr"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	"github.com/vertica/vertica-kubernetes/pkg/security"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	// +kubebuilder:scaffold:imports
)

const (
	CertDir = "/tmp/k8s-webhook-server/serving-certs"
)

//go:generate sh -c "printf %s $(git rev-parse HEAD) > git-commit.go-generate.txt"
//go:generate sh -c "printf %s $(date +%Y-%m-%dT%T -u) > build-date.go-generate.txt"
//go:generate sh -c "printf %s $(go list -m -f '{{ .Version }}' github.com/vertica/vcluster) > vcluster-version.go-generate.txt"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	//go:embed git-commit.go-generate.txt
	GitCommit string
	//go:embed build-date.go-generate.txt
	BuildDate string
	//go:embed vcluster-version.go-generate.txt
	VClusterVersion string
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(vapiB1.AddToScheme(scheme))
	utilruntime.Must(vapiV1.AddToScheme(scheme))
	utilruntime.Must(kedav1alpha1.AddToScheme(scheme))
	utilruntime.Must(promv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// addReconcilersToManager will add a controller for each CR that this operator
// handles.  If any failure occurs, if will exit the program.
func addReconcilersToManager(mgr manager.Manager, restCfg *rest.Config) {
	if !opcfg.GetIsControllersEnabled() {
		setupLog.Info("Controllers are disabled")
		return
	}

	cacheManager := vcache.MakeCacheManager(opcfg.GetIsCacheEnabled())
	// Create a custom option with our own rate limiter
	rateLimiter := workqueue.NewItemExponentialFailureRateLimiter(1*time.Millisecond,
		time.Duration(opcfg.GetVdbMaxBackoffDuration())*time.Millisecond)
	options := controller.Options{
		RateLimiter: rateLimiter,
	}
	if err := (&vdb.VerticaDBReconciler{
		Client:       mgr.GetClient(),
		Log:          ctrl.Log.WithName("controllers").WithName("VerticaDB"),
		Scheme:       mgr.GetScheme(),
		Cfg:          restCfg,
		EVRec:        mgr.GetEventRecorderFor(vmeta.OperatorName),
		CacheManager: cacheManager,
	}).SetupWithManager(mgr, options); err != nil {
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
	if err := (&vrpq.VerticaRestorePointsQueryReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		EVRec:        mgr.GetEventRecorderFor(vmeta.OperatorName),
		Log:          ctrl.Log.WithName("controllers").WithName("VerticaRestorePointsQuery"),
		CacheManager: cacheManager,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VerticaRestorePointsQuery")
		os.Exit(1)
	}
	if err := (&vscr.VerticaScrutinizeReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Cfg:    restCfg,
		EVRec:  mgr.GetEventRecorderFor(vmeta.OperatorName),
		Log:    ctrl.Log.WithName("controllers").WithName("VerticaScrutinize"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VerticaScrutinize")
		os.Exit(1)
	}
	sbRateLimiter := workqueue.NewItemExponentialFailureRateLimiter(1*time.Millisecond,
		time.Duration(opcfg.GetSandboxMaxBackoffDuration())*time.Millisecond)
	sbOptions := controller.Options{
		RateLimiter: sbRateLimiter,
	}
	if err := (&sandbox.SandboxConfigMapReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Cfg:          restCfg,
		EVRec:        mgr.GetEventRecorderFor(vmeta.OperatorName),
		Log:          ctrl.Log.WithName("controllers").WithName("sandbox"),
		Concurrency:  opcfg.GetSandboxConfigMapConcurrency(),
		CacheManager: cacheManager,
	}).SetupWithManager(mgr, &sbOptions); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "sandbox")
		os.Exit(1)
	}
	if err := (&vrep.VerticaReplicatorReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Cfg:          restCfg,
		EVRec:        mgr.GetEventRecorderFor(vmeta.OperatorName),
		Log:          ctrl.Log.WithName("controllers").WithName("VerticaReplicator"),
		Concurrency:  opcfg.GetVerticaReplicatorConcurrency(),
		CacheManager: cacheManager,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VerticaReplicator")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder
}

// addWebhooktsToManager will add any webhooks to the manager.  If any failure
// occurs, it will exit the program.
func addWebhooksToManager(mgr manager.Manager) {
	if err := (&vapiV1.VerticaDB{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "VerticaDB", "version", vapiV1.Version)
	}
	if err := (&vapiV1.VerticaAutoscaler{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "VerticaAutoscaler", "version", vapiV1.Version)
		os.Exit(1)
	}
	if err := (&vapiB1.EventTrigger{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "EventTrigger", "version", vapiB1.Version)
		os.Exit(1)
	}
	if err := (&vapiB1.VerticaRestorePointsQuery{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "VerticaRestorePointsQuery", "version", vapiB1.Version)
		os.Exit(1)
	}
	if err := (&vapiB1.VerticaScrutinize{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "VerticaScrutinize", "version", vapiB1.Version)
		os.Exit(1)
	}
	if err := (&vapiB1.VerticaReplicator{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "VerticaReplicator", "version", vapiB1.Version)
		os.Exit(1)
	}
}

// setupWebhook will setup the webhook in the manager if enabled
func setupWebhook(ctx context.Context, mgr manager.Manager, restCfg *rest.Config) error {
	if opcfg.GetIsWebhookEnabled() {
		ns := opcfg.GetOperatorNamespace()
		if opcfg.GetWebhookCertSecret() == "" {
			setupLog.Info("generating webhook cert")
			if err := security.GenerateWebhookCert(ctx, &setupLog, restCfg, CertDir, opcfg.GetPrefixName(), ns); err != nil {
				return err
			}
		} else if opcfg.GetIsOLMDeployment() {
			// OLM will generate the cert themselves and they have their own
			// mechanism to update the webhook configs and conversion webhook in the CRD.
			setupLog.Info("OLM deployment detected. Skipping webhook cert update")
		} else if !opcfg.GetUseCertManager() {
			setupLog.Info("using provided webhook cert", "secret", opcfg.GetWebhookCertSecret())
			if err := security.PatchWebhookCABundleFromSecret(ctx, &setupLog, restCfg, opcfg.GetWebhookCertSecret(),
				opcfg.GetPrefixName(), ns); err != nil {
				return err
			}
		} else {
			setupLog.Info("using cert-manager for webhook cert")
			if err := security.AddCertManagerAnnotation(ctx, &setupLog, restCfg, opcfg.GetPrefixName(), ns); err != nil {
				return err
			}
		}
		addWebhooksToManager(mgr)
	} else {
		setupLog.Info("webhook setup is because webhook is not enabled")
	}
	return nil
}

// getReadinessProbeCallack returns the check to use for the readiness probe
func getReadinessProbeCallback(mgr ctrl.Manager) healthz.Checker {
	// If the webhook is enabled, we use a checker that tests if the webhook is
	// able to accept requests.
	if opcfg.GetIsWebhookEnabled() {
		return mgr.GetWebhookServer().StartedChecker()
	}
	return healthz.Ping
}

//nolint:funlen
func main() {
	logger := opcfg.GetLogger()
	if opcfg.GetLoggingFilePath() != "" {
		log.Printf("Now logging in file %s", opcfg.GetLoggingFilePath())
	}
	burstSize := opcfg.GetBroadcasterBurstSize()
	var multibroadcaster = record.NewBroadcasterWithCorrelatorOptions(record.CorrelatorOptions{BurstSize: burstSize})
	ctrl.SetLogger(logger)
	setupLog.Info("Build info", "gitCommit", GitCommit,
		"buildDate", BuildDate, "vclusterVersion", VClusterVersion)
	setupLog.Info("Operator Config",
		"controllersScope", opcfg.GetControllersScope(),
		"version", opcfg.GetVersion(),
		"watchNamespace", opcfg.GetWatchNamespace(),
		"webhooksEnabled", opcfg.GetIsWebhookEnabled(),
		"cacheEnabled", opcfg.GetIsCacheEnabled(),
		"controllersEnabled", opcfg.GetIsControllersEnabled(),
		"broadcasterBurstSize", burstSize,
		"monitoringEnabled", opcfg.IsPrometheusEnabled(),
	)

	var webhookTLSOpts []func(*tls.Config)
	var metricsTLSOpts []func(*tls.Config)
	// Set the minimum TLS version for the webhook.  By default it will use
	// TLS 1.0, which has a lot of security flaws.  This is a hacky way to
	// set this and should be removed once there is a supported way.
	// There are numerous proposals to allow this to be configured from
	// Manager -- based on most recent activity this one looks promising:
	// https://github.com/kubernetes-sigs/controller-runtime/issues/852
	webhookTLSOpts = append(webhookTLSOpts, func(c *tls.Config) {
		c.MinVersion = tls.VersionTLS13
	})
	metricsTLSOpts = append(metricsTLSOpts, func(c *tls.Config) {
		c.MinVersion = tls.VersionTLS13
	})

	webhookServer := webhook.NewServer(webhook.Options{
		Port:    9443,
		CertDir: CertDir,
		TLSOpts: webhookTLSOpts,
	})

	secureByAuth := opcfg.IfSecureByAuth()
	secureByTLS := opcfg.IfSecureByTLS()
	var metricCertDir string
	if opcfg.GetMetricsTLSSecret() != "" {
		metricCertDir = "/cert"
		metricsTLSOpts = append(metricsTLSOpts, func(c *tls.Config) {
			// Load the CA certificate
			caCert, err := os.ReadFile("/cert/ca.crt")
			if err != nil {
				log.Fatalf("failed to read CA cert: %v", err)
			}
			// Create a CertPool and add the CA certificate to it
			caCertPool := x509.NewCertPool()
			ok := caCertPool.AppendCertsFromPEM(caCert)
			if !ok {
				log.Fatal("failed to append CA cert to CertPool")
			}
			c.ClientCAs = caCertPool
			// If we enabled authorization, then no client certs are really needed.
			// Otherwise, we need the client certs.
			if secureByAuth {
				c.ClientAuth = tls.VerifyClientCertIfGiven
			} else if secureByTLS {
				c.ClientAuth = tls.RequireAndVerifyClientCert
			}
		})
	}

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   opcfg.GetMetricsAddr(),
		SecureServing: secureByAuth || secureByTLS,
		// TLSOpts is used to allow configuring the TLS config used for the server. If certificates are
		// not provided, self-signed certificates will be generated by default. This option is not recommended for
		// production environments as self-signed certificates do not offer the same level of trust and security
		// as certificates issued by a trusted Certificate Authority (CA). The primary risk is potentially allowing
		// unauthorized access to sensitive metrics data. Consider replacing with CertDir, CertName, and KeyName
		// to provide certificates, ensuring the server communicates using trusted and secure certificates.
		TLSOpts: metricsTLSOpts,
		CertDir: metricCertDir,
	}

	if secureByAuth {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	restCfg := ctrl.GetConfigOrDie()

	var cacheNamespaces map[string]cache.Config
	if opcfg.GetWatchNamespace() != "" {
		cacheNamespaces = make(map[string]cache.Config)
		cacheNamespaces[opcfg.GetWatchNamespace()] = cache.Config{}
	}
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsServerOptions,
		WebhookServer:           webhookServer,
		HealthProbeBindAddress:  ":8081",
		LeaderElection:          true,
		LeaderElectionID:        opcfg.GetLeaderElectionID(),
		LeaderElectionNamespace: opcfg.GetOperatorNamespace(),
		Cache:                   cache.Options{DefaultNamespaces: cacheNamespaces},
		EventBroadcaster:        multibroadcaster,
		Controller: config.Controller{
			GroupKindConcurrency: map[string]int{
				vapiB1.GkVDB.String():  opcfg.GetVerticaDBConcurrency(),
				vapiB1.GkVAS.String():  opcfg.GetVerticaAutoscalerConcurrency(),
				vapiB1.GkET.String():   opcfg.GetEventTriggerConcurrency(),
				vapiB1.GkVRPQ.String(): opcfg.GetVerticaRestorePointsQueryConcurrency(),
				vapiB1.GkVSCR.String(): opcfg.GetVerticaScrutinizeConcurrency(),
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	addReconcilersToManager(mgr, restCfg)
	ctx := ctrl.SetupSignalHandler()
	if err := setupWebhook(ctx, mgr, restCfg); err != nil {
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
