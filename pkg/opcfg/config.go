/* (c) Copyright [2021-2024] Open Text.
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
	"os"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// GetIsWebhookEnabled returns true if the webhook is enabled.
func GetIsWebhookEnabled() bool {
	return lookupBoolEnvVar("ENABLE_WEBHOOKS", envMustExist)
}

// GetIsControllersEnabled returns true if the controllers for each custom
// resource will start. If this is false, then the manager will just act as a
// webhook (if enabled).
func GetIsControllersEnabled() bool {
	return lookupBoolEnvVar("ENABLE_CONTROLLERS", envMustExist)
}

// GetWatchNamespace returns the namespace that the operator should watch
func GetWatchNamespace() string {
	// The watch namespace depends on the scope of the operator.
	scope := GetOperatorScope()
	if scope == "namespace" {
		// A namespace scoped operator only watches for objects in the namespace
		// it is deployed in.
		return GetOperatorNamespace()
	}
	// A cluster scoped operator. Return an empty string so that all namespaces
	// are watched.
	return ""
}

// GetOperatorScope returns the scope, cluster or namespace, of the operator.
func GetOperatorScope() string {
	return lookupStringEnvVar("OPERATOR_SCOPE", envMustExist)
}

// GetMetricsAddr returns the address of the manager's Prometheus endpoint. This
// determines if its disabled or if its behind an HTTPS or HTTP scheme.
func GetMetricsAddr() string {
	return lookupStringEnvVar("METRICS_ADDR", envCanNotExist)
}

// GetIsProfilerEnabled returns true if the memory profiler started with the
// manager.
func GetIsProfilerEnabled() bool {
	return lookupBoolEnvVar("ENABLE_PROFILER", envMustExist)
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

// getIsLoggingSamplingEnabled returns true if sampling is to be used to cut
// down on the CPU and I/O costs of the logging.
func getIsLoggingSamplingEnabled() bool {
	return lookupBoolEnvVar("ENABLE_LOG_SAMPLING", envMustExist)
}

// GetLoggingLevel returns the logging level to use. 0 is the level with the
// least amount of output. Anything higher will have more verbose output.
func getLoggingLevel() int {
	lvl := lookupStringEnvVar("LOG_LEVEL", envMustExist)
	// In case we use the old named levels, we will tolerate errors when
	// converting to int. We will just default to 0 in that case.
	intLevel, err := strconv.Atoi(lvl)
	if err != nil {
		return 0
	}
	return intLevel
}

// getUseProductionLoggingConfig returns true if production logging output
// should be generated. This produces a slightly more terse output than dev
// logging.
func getUseProductionLoggingConfig() bool {
	return lookupBoolEnvVar("LOG_USE_PRODUCTION_CONFIG", envCanNotExist)
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
	return lookupStringEnvVar("DEPLOY_WITH", envMustExist)
}

// GetVersion returns the version of the operator.
func GetVersion() string {
	return lookupStringEnvVar("VERSION", envMustExist)
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
// that the operator can only once in a namespace.
func GetLeaderElectionID() string {
	// We need to have a separate ID if the webhook running is decoupled from
	// the controllers. This allows both of them to co-exist at the same time.
	if GetIsControllersEnabled() {
		return "5c1e6227.vertica.com"
	}
	return "87f832c4.vertica.com"
}

func dieIfNotFound(envName string) {
	fmt.Fprintf(os.Stderr, "*** ERROR: Environment variable %s not found.", envName)
	os.Exit(1)
}

func dieIfNotValid(envName, rawVal string) {
	fmt.Fprintf(os.Stderr, "*** ERROR: Invalid value %q for environment variable %s", rawVal, envName)
	os.Exit(1)
}

const (
	// Helper consts for the mustExist parameter for the lookup*EnvVar functions.
	envMustExist   = true
	envCanNotExist = false
)

// lookupBoolEnvVar will look for an environment variable and return its value
// as if it's a boolean. Any errors will stop the manager.
func lookupBoolEnvVar(envName string, mustExist bool) bool {
	valStr, found := os.LookupEnv(envName)
	if !found {
		if mustExist {
			dieIfNotFound(envName)
		}
		return false
	}
	valBool, err := strconv.ParseBool(valStr)
	if err != nil {
		dieIfNotValid(envName, valStr)
		return false
	}
	return valBool
}

// lookupStringEnvVar will look for an environment variable and return its value
// as a string. Any errors will stop the manager.
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

// lookupBoolEnvVar will look for an environment variable and return its value
// as if it's a boolean. Any errors will stop the manager.
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
		dieIfNotValid(envName, valStr)
		return 0
	}
	return valInt
}

// getEncoderConfig returns a concrete encoders configuration
func getEncoderConfig() zapcore.EncoderConfig {
	if getUseProductionLoggingConfig() {
		encoderConfig := zap.NewProductionEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		return encoderConfig
	}
	return zap.NewDevelopmentEncoderConfig()
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
	if GetLoggingFilePath() == "" || !getUseProductionLoggingConfig() {
		writes = append(writes, zapcore.AddSync(os.Stdout))
	}
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(writes...),
		lvl,
	)
	opts = append(opts, getStackTrace())
	if getIsLoggingSamplingEnabled() {
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

// getZapcoreLevel return the logging level to use for the logging. Levels are
// numbered. 0 is the minimum amount of logging. Higher numbers will dump out
// more verbose messages.
func getZapcoreLevel() zapcore.Level {
	return zapcore.Level(getLoggingLevel())
}

// getStackTrace returns an option that configures
// the logger to record a stack strace.
func getStackTrace() zap.Option {
	lvl := zapcore.WarnLevel
	if getUseProductionLoggingConfig() {
		lvl = zapcore.ErrorLevel
	}
	return zap.AddStacktrace(zapcore.LevelEnabler(lvl))
}
