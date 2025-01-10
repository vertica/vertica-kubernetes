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

package opcfg

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

const defaultBurstSize = 25

// GetIsWebhookEnabled returns true if the webhook is enabled.
func GetIsWebhookEnabled() bool {
	return lookupBoolEnvVar("WEBHOOKS_ENABLED", envMustExist)
}

// GetBroadcasterBurstSize returns the customizable burst size for broadcaster.
func GetBroadcasterBurstSize() int {
	burstSize := lookupIntEnvVar("BROADCASTER_BURST_SIZE", envCanNotExist)
	if burstSize > defaultBurstSize {
		return burstSize
	}
	return defaultBurstSize
}

// GetIsControllersEnabled returns true if the controllers for each custom
// resource will start. If this is false, then the manager will just act as a
// webhook (if enabled).
func GetIsControllersEnabled() bool {
	return lookupBoolEnvVar("CONTROLLERS_ENABLED", envMustExist)
}

// GetWatchNamespace returns the namespace that the operator should watch
func GetWatchNamespace() string {
	// The watch namespace depends on the scope of the controllers.
	if AreControllersNamespaceScoped() {
		// A namespace scoped controller only watches for objects in the namespace
		// it is deployed in.
		return GetOperatorNamespace()
	}
	// A cluster scoped controller. Return an empty string so that all namespaces
	// are watched.
	return ""
}

const ControllersScopeEnvVar = "CONTROLLERS_SCOPE" // Environment variable used to set scope of the controllers
const ControllersScopeCluster = "cluster"
const controllersScopeNamespace = "namespace"

// GetControllersScope returns the scope, cluster or namespace, of the operator.
func GetControllersScope() string {
	val := lookupStringEnvVar(ControllersScopeEnvVar, envCanNotExist)
	if val != controllersScopeNamespace {
		return ControllersScopeCluster
	}
	return val
}

// AreControllersNamespaceScoped returns true if the controllers only watch a
// single namespace.
func AreControllersNamespaceScoped() bool {
	scope := GetControllersScope()
	return scope == controllersScopeNamespace
}

// GetMetricsAddr returns the address of the manager's Prometheus endpoint. This
// determines if its disabled or if its behind an HTTPS or HTTP scheme.
func GetMetricsAddr() string {
	return lookupStringEnvVar("METRICS_ADDR", envCanNotExist)
}

// GetUseCertManager returns true if cert-manager is used to setup the webhook's
// TLS certs.
func GetUseCertManager() bool {
	source := lookupStringEnvVar("WEBHOOK_CERT_SOURCE", envMustExist)
	return source == "cert-manager"
}

// GetLoggingFilePath returns the full path to the log file. If this is empty,
// then logging will be written to the console only.
func GetLoggingFilePath() string {
	return lookupStringEnvVar("LOG_FILE_PATH", envCanNotExist)
}

// getLoggingMaxFileSize will return the size of the log file when writing logs
// to a file.
func getLoggingMaxFileSize() int {
	return lookupIntEnvVar("LOG_MAX_FILE_SIZE", envCanNotExist)
}

// getLoggingMaxFileAge will return the age, in days, an log file stays around.
// This only applies if writing logs to a file.
func getLoggingMaxFileAge() int {
	return lookupIntEnvVar("LOG_MAX_FILE_AGE", envCanNotExist)
}

// getLoggingMaxFileRotation will determine how many rotated log files it will
// keep around. This only applies if logging to a file.
func getLoggingMaxFileRotation() int {
	return lookupIntEnvVar("LOG_MAX_FILE_ROTATION", envCanNotExist)
}

// GetLoggingLevel returns the logging level to use. Logging levels are: debug,
// info, warn, error.
func getLoggingLevel() string {
	return lookupStringEnvVar("LOG_LEVEL", envCanNotExist)
}

// IsDebugLoggingEnabled returns true if the debug logging level is selected.
func IsDebugLoggingEnabled() bool {
	lvl := getLoggingLevel()
	return lvl == "debug"
}

// GetVerticaDBConcurrency returns the number of goroutines that will service
// VerticaDB CRs.
func GetVerticaDBConcurrency() int {
	return lookupIntEnvVar("CONCURRENCY_VERTICADB", envMustExist)
}

// GetVerticaAutoscalerConcurrency returns the number of goroutines that will
// service VerticaAutoscaler CRs.
func GetVerticaAutoscalerConcurrency() int {
	return lookupIntEnvVar("CONCURRENCY_VERTICAAUTOSCALER", envMustExist)
}

// GetEventTriggerConcurrency returns the number of goroutines that will service
// EventTrigger CRs.
func GetEventTriggerConcurrency() int {
	return lookupIntEnvVar("CONCURRENCY_EVENTTRIGGER", envMustExist)
}

// GetVerticaRestorePointsQueryConcurrency returns the number of goroutines that
// will service VerticaRestorePointsQuery CRs.
func GetVerticaRestorePointsQueryConcurrency() int {
	return lookupIntEnvVar("CONCURRENCY_VERTICARESTOREPOINTSQUERY", envMustExist)
}

// GetVerticaScrutinizeConcurrency returns the number of goroutines that will
// service VerticaScrutinize CRs.
func GetVerticaScrutinizeConcurrency() int {
	return lookupIntEnvVar("CONCURRENCY_VERTICASCRUTINIZE", envMustExist)
}

// GetSandboxConfigMapConcurrency returns the number of goroutines that will
// service sandbox ConfigMaps
func GetSandboxConfigMapConcurrency() int {
	return lookupIntEnvVar("CONCURRENCY_SANDBOXCONFIGMAP", envMustExist)
}

// GetVerticaReplicatorConcurrency returns the number of goroutines that will
// service VerticaReplicator CRs.
func GetVerticaReplicatorConcurrency() int {
	return lookupIntEnvVar("CONCURRENCY_VERTICAREPLICATOR", envMustExist)
}

// GetPrefixName returns the common prefix for all objects used to deploy the
// operator.
func GetPrefixName() string {
	return lookupStringEnvVar("PREFIX_NAME", envMustExist)
}

// GetIsOLMDeployment returns true if operator lifecylce manager (OLM) was used
// to deploy the operator.
func GetIsOLMDeployment() bool {
	deployWith := GetDeploymentMethod()
	return deployWith == "olm"
}

// GetDeploymentMethod returns the name of the method that was used to deploy
// the operator.
func GetDeploymentMethod() string {
	return lookupStringEnvVar("DEPLOY_WITH", envCanNotExist)
}

// GetVersion returns the version of the operator.
func GetVersion() string {
	return lookupStringEnvVar("VERSION", envCanNotExist)
}

// GetWebhookCertSecret returns the name of the secret that stores the TLS cert
// for the webhook.
func GetWebhookCertSecret() string {
	return lookupStringEnvVar("WEBHOOK_CERT_SECRET", envMustExist)
}

// GetOperatorNamespace retrieves the namespace that the operator is running in
func GetOperatorNamespace() string {
	return lookupStringEnvVar("OPERATOR_NAMESPACE", envMustExist)
}

// GetLeaderElectionID returns the name to use for leader election. This ensures
// that the operator can only run once in a namespace.
func GetLeaderElectionID() string {
	// We need to have a separate ID if the webhook running is decoupled from
	// the controllers. This allows both of them to co-exist at the same time.
	if GetIsControllersEnabled() {
		return "5c1e6227.vertica.com"
	}
	return "87f832c4.vertica.com"
}

// GetDevMode returns true if logging is enabled for dev mode.
func GetDevMode() bool {
	return lookupBoolEnvVar("DEV_MODE", envCanNotExist)
}

func dieIfNotFound(envName string) {
	fmt.Fprintf(os.Stderr, "*** ERROR: Environment variable %s not found.", envName)
	os.Exit(1)
}

func dieIfNotValid(envName string) {
	fmt.Fprintf(os.Stderr, "*** ERROR: Invalid value for environment variable %s", envName)
	os.Exit(1)
}

const (
	// Helper consts for the mustExist parameter for the lookup*EnvVar functions.
	envMustExist   = true  // The environment variable must exist. Manager stops if not found.
	envCanNotExist = false // The environment variable is optional. No error is generated if not found.
)

// lookupBoolEnvVar will look for an environment variable and return its value
// as if it's a boolean. If mustExist is true and the variable isn't found, the
// manager is stopped.
func lookupBoolEnvVar(envName string, mustExist bool) bool {
	valStr, found := os.LookupEnv(envName)
	if !found {
		if mustExist {
			dieIfNotFound(envName)
		}
		return false
	}
	if valStr == "" {
		return false
	}
	valBool, err := strconv.ParseBool(valStr)
	if err != nil {
		dieIfNotValid(envName)
		return false
	}
	return valBool
}

// lookupStringEnvVar will look for an environment variable and return its value
// as a string. If mustExist is true and the variable isn't found, the manager
// is stopped.
func lookupStringEnvVar(envName string, mustExist bool) string {
	valStr, found := os.LookupEnv(envName)
	if !found {
		if mustExist {
			dieIfNotFound(envName)
		}
		return ""
	}
	return valStr
}

// lookupIntEnvVar will look for an environment variable and return its value
// as if it's an integer. If mustExist is true and the variable isn't found, the
// manager is stopped.
func lookupIntEnvVar(envName string, mustExist bool) int {
	valStr, found := os.LookupEnv(envName)
	if !found {
		if mustExist {
			dieIfNotFound(envName)
		}
		return 0
	}
	valInt, err := strconv.Atoi(valStr)
	if err != nil {
		dieIfNotValid(envName)
		return 0
	}
	return valInt
}

// getEncoderConfig returns a concrete encoders configuration
func getEncoderConfig() zapcore.EncoderConfig {
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	if !GetDevMode() {
		encoderConfig = zap.NewProductionEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}
	return encoderConfig
}

// getLogger is a wrapper that calls other functions
// to build a logger.
func GetLogger() logr.Logger {
	encoderConfig := getEncoderConfig()
	writes := []zapcore.WriteSyncer{}
	opts := []zap.Option{}
	lvl := zap.NewAtomicLevelAt(getZapcoreLevel())
	if GetLoggingFilePath() != "" {
		w := getLogWriter()
		writes = append(writes, w)
	}
	if GetLoggingFilePath() == "" || GetDevMode() {
		writes = append(writes, zapcore.AddSync(os.Stdout))
	}
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(writes...),
		lvl,
	)
	opts = append(opts, getStackTrace())
	if !GetDevMode() {
		const first = 100
		const thereAfter = 100
		core = zapcore.NewSamplerWithOptions(core, time.Second, first, thereAfter)
	}
	return zapr.NewLogger(zap.New(core, opts...))
}

// getLogWriter returns an io.writer (setting up rolling files) converted
// into a zapcore.WriteSyncer
func getLogWriter() zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   GetLoggingFilePath(),
		MaxSize:    getLoggingMaxFileSize(), // megabytes
		MaxBackups: getLoggingMaxFileRotation(),
		MaxAge:     getLoggingMaxFileAge(), // days
	}
	return zapcore.AddSync(lumberJackLogger)
}

// getZapcoreLevel return the zapcore level to use for the logging. Levels are
// taken from string level, via getLoggingLevel, and mapped to its corresponding
// zapcore level. If the string level is invalid, it returns the default level.
func getZapcoreLevel() zapcore.Level {
	const (
		DefaultZapcoreLevel = zapcore.InfoLevel
		DefaultLevel        = "info"
	)
	var level = new(zapcore.Level)
	err := level.UnmarshalText([]byte(getLoggingLevel()))
	if err != nil {
		log.Printf("unrecognized level, %s level will be used instead", DefaultLevel)
		return DefaultZapcoreLevel
	}
	return *level
}

// getStackTrace returns an option that configures
// the logger to record a stack strace.
func getStackTrace() zap.Option {
	lvl := zapcore.WarnLevel
	if GetDevMode() {
		lvl = zapcore.ErrorLevel
	}
	return zap.AddStacktrace(zapcore.LevelEnabler(lvl))
}

// GetVdbMaxBackoffDuration returns maximum backoff requeue duration in milliseconds for vdb controller
func GetVdbMaxBackoffDuration() int {
	const envName = "VDB_MAX_BACKOFF_DURATION"
	duration := lookupStringEnvVar(envName, envCanNotExist)
	if duration == "" {
		// Default value is 1000ms
		return 1000
	}
	valInt, err := strconv.Atoi(duration)
	if err != nil || valInt < 0 {
		dieIfNotValid(envName)
	}
	return valInt
}
