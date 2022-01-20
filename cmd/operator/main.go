/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"net/http"
	_ "net/http/pprof" // nolint:gosec

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	verticacomv1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	//+kubebuilder:scaffold:imports
)

const (
	MaxFileSize     = 500
	MaxFileAge      = 7
	MaxFileRotation = 3
	Level           = "info"
	DevMode         = true
	Stdout          = false
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

type Logging struct {
	FilePath        string
	Level           string
	MaxFileSize     int
	MaxFileAge      int
	MaxFileRotation int
	Stdout          bool
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(verticacomv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// getWatchNamespace returns the Namespace the operator should be watching for changes
func getWatchNamespace() (string, error) {
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which specifies the Namespace to watch.
	// An empty value means the operator is running with cluster scope.
	var watchNamespaceEnvVar = "WATCH_NAMESPACE"

	ns, found := os.LookupEnv(watchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", watchNamespaceEnvVar)
	}
	return ns, nil
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
	if err != nil {
		return DefaultEnabled
	}
	return enabled
}

// getEncoderConfig returns a concrete encoders configuration
func getEncoderConfig(devMode bool) zapcore.EncoderConfig {
	encoderConfig := zap.NewProductionEncoderConfig()
	if devMode {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
	}
	return encoderConfig
}

// getLogWriter returns an io.writer (setting up rolling files) converted
// into a zapcore.WriteSyncer
func getLogWriter(path string, age, size, rotation int) zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    size, // megabytes
		MaxBackups: rotation,
		MaxAge:     age, // days
	}
	return zapcore.AddSync(lumberJackLogger)
}

// getZapcoreLevel takes the level as string and returns the corresponding
// zapcore.Level. If the string level is invalid, it returns the default
// level
func getZapcoreLevel(lvl string) zapcore.Level {
	lvls := map[string]zapcore.Level{
		"debug": zapcore.DebugLevel,
		"info":  zapcore.InfoLevel,
		"warn":  zapcore.WarnLevel,
		"error": zapcore.ErrorLevel,
	}
	if _, found := lvls[lvl]; !found {
		// returns default level if the input level is invalid
		log.Println(fmt.Sprintf("Invalid level, %s level will be used instead", Level))
		return lvls[Level]
	}
	return lvls[lvl]
}

// getLogger is a wrapper that calls other functions
// to build a logger.
func getLogger(logArgs Logging, devMode bool) *zap.Logger {
	encoderConfig := getEncoderConfig(devMode)
	writes := []zapcore.WriteSyncer{}
	lvl := zap.NewAtomicLevelAt(zap.DebugLevel)
	if logArgs.FilePath != "" {
		w := getLogWriter(logArgs.FilePath, logArgs.MaxFileAge, logArgs.MaxFileSize, logArgs.MaxFileRotation)
		lvl = zap.NewAtomicLevelAt(getZapcoreLevel(logArgs.Level))
		writes = append(writes, w)
	}
	if logArgs.FilePath == "" || logArgs.Stdout {
		writes = append(writes, zapcore.AddSync(os.Stdout))
	}
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(writes...),
		lvl,
	)
	return zap.New(core)
}

// nolint:funlen
func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var enableProfiler bool
	var devMode bool
	logArgs := Logging{}
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableProfiler, "enable-profiler", false,
		"Enables runtime profiling collection.  The profiling data can be inspected by connecting to port 6060 "+
			"with the path /debug/pprof.  See https://golang.org/pkg/net/http/pprof/ for more info.")
	flag.StringVar(&logArgs.FilePath, "filepath", "", "The file logging will write to.")
	flag.IntVar(&logArgs.MaxFileSize, "maxfilesize", MaxFileSize,
		"The maximum size in megabytes of the log file "+
			"before it gets rotated.")
	flag.IntVar(&logArgs.MaxFileAge, "maxfileage", MaxFileAge,
		"This is the maximum age, in days, of the logging "+
			"before log rotation gets rid of it.")
	flag.IntVar(&logArgs.MaxFileRotation, "maxfilerotation", MaxFileRotation,
		"this is the maximum number of files that are kept in rotation before the old ones are removed.")
	flag.StringVar(&logArgs.Level, "level", Level, "The minimum logging level.  Valid values are: debug, info, warn, and error.")
	flag.BoolVar(&devMode, "dev", DevMode, "Enables development mode if true and production mode otherwise.")
	flag.BoolVar(&logArgs.Stdout, "stdout", Stdout, "Enables logging to stdout.")
	flag.Parse()

	logger := getLogger(logArgs, devMode)
	if logArgs.FilePath != "" {
		log.Println(fmt.Sprintf("Now logging in file %s", logArgs.FilePath))
	}

	ctrl.SetLogger(zapr.NewLogger(logger))

	if enableProfiler {
		go func() {
			addr := "localhost:6060"
			setupLog.Info("Opening profiling port", "addr", addr)
			if err := http.ListenAndServe(addr, nil); err != nil {
				setupLog.Error(err, "Profiling port closed")
			}
		}()
	}

	restCfg := ctrl.GetConfigOrDie()

	watchNamespace, err := getWatchNamespace()
	if err != nil {
		setupLog.Info("unable to get WatchNamespace, " +
			"the manager will watch and manage resources in all namespaces")
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "5c1e6227.vertica.com",
		Namespace:              watchNamespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.VerticaDBReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("VerticaDB"),
		Scheme: mgr.GetScheme(),
		Cfg:    restCfg,
		EVRec:  mgr.GetEventRecorderFor(controllers.OperatorName),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VerticaDB")
		os.Exit(1)
	}

	if getIsWebhookEnabled() {
		if err = (&verticacomv1beta1.VerticaDB{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "VerticaDB")
			os.Exit(1)
		}
		//+kubebuilder:scaffold:builder
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
