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

package opcfg

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

const (
	DefaultZapcoreLevel                        = zapcore.InfoLevel
	First                                      = 100
	ThereAfter                                 = 100
	DefaultMaxFileSize                         = 500
	DefaultMaxFileAge                          = 7
	DefaultMaxFileRotation                     = 3
	DefaultLevel                               = "info"
	DefaultDevMode                             = true
	DefaultVerticaDBConcurrency                = 5
	DefaultVerticaAutoscalerDBConcurrency      = 1
	DefaultEventTriggerDBConcurrency           = 1
	DefaultVerticaRestorePointQueryConcurrency = 1
)

type OperatorConfig struct {
	MetricsAddr          string
	EnableLeaderElection bool
	ProbeAddr            string
	EnableProfiler       bool
	PrefixName           string // Prefix of the name of all objects created when the operator was deployed
	WebhookCertSecret    string // when this is empty we will generate the webhook cert
	DevMode              bool
	// This is set if the webhook is enabled and its TLS is set via
	// cert-manager. cert-manager will handle the CA bundle injection in the
	// various k8s objects.
	UseCertManager bool
	// The *Currency parms control the concurrency of go routines handling each
	// CR.  For instance, VerticaDBConcurrency is the number of go routines to
	// handle reconciliation of VerticaDB CRs.
	VerticaDBConcurrency                int
	VerticaAutoscalerConcurrency        int
	EventTriggerConcurrency             int
	VerticaRestorePointQueryConcurrency int
	Logging
}

type Logging struct {
	FilePath        string
	Level           string
	MaxFileSize     int
	MaxFileAge      int
	MaxFileRotation int
}

// SetFlagArgs define flags with specified names and default values
func (o *OperatorConfig) SetFlagArgs() {
	flag.StringVar(&o.MetricsAddr, "metrics-bind-address", "0",
		"The address the metric endpoint binds to. Setting this to 0 will disable metric serving.")
	flag.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&o.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&o.EnableProfiler, "enable-profiler", false,
		"Enables runtime profiling collection.  The profiling data can be inspected by connecting to port 6060 "+
			"with the path /debug/pprof.  See https://golang.org/pkg/net/http/pprof/ for more info.")
	flag.StringVar(&o.PrefixName, "prefix-name", "verticadb-operator",
		"The common prefix for all objects created during the operator deployment")
	flag.StringVar(&o.WebhookCertSecret, "webhook-cert-secret", "",
		"Specifies the secret that contains the webhook cert. If this option is omitted, "+
			"then the operator will generate the certificate.")
	flag.BoolVar(&o.UseCertManager, "use-cert-manager", false,
		"If the operator uses cert-manager to generate the TLS for the webhook.")
	flag.BoolVar(&o.DevMode, "dev", DefaultDevMode,
		"Enables development mode if true and production mode otherwise.")
	flag.StringVar(&o.FilePath, "filepath", "",
		"The path to the log file. If omitted, all logging will be written to stdout.")
	flag.IntVar(&o.MaxFileSize, "maxfilesize", DefaultMaxFileSize,
		"The maximum size in megabytes of the log file "+
			"before it gets rotated.")
	flag.IntVar(&o.MaxFileAge, "maxfileage", DefaultMaxFileAge,
		"The maximum number of days to retain old log files based on the timestamp encoded in the file.")
	flag.IntVar(&o.MaxFileRotation, "maxfilerotation", DefaultMaxFileRotation,
		"The maximum number of files that are kept in rotation before the old ones are removed.")
	flag.StringVar(&o.Level, "level", DefaultLevel,
		"The minimum logging level.  Valid values are: debug, info, warn, and error.")
	flag.IntVar(&o.VerticaDBConcurrency, "verticadb-concurrency", DefaultVerticaDBConcurrency,
		"The amount of concurrency to reconcile VerticaDB CRs")
	flag.IntVar(&o.VerticaAutoscalerConcurrency, "verticaautoscaler-concurrency",
		DefaultVerticaAutoscalerDBConcurrency,
		"The amount of concurrency to reconcile VerticaAutoscaler CRs")
	flag.IntVar(&o.EventTriggerConcurrency, "eventtrigger-concurrency", DefaultEventTriggerDBConcurrency,
		"The amount of concurrency to reconcile EventTrigger CRs")
	flag.IntVar(&o.VerticaRestorePointQueryConcurrency, "verticarestorepoint-concurrency", DefaultVerticaRestorePointQueryConcurrency,
		"The amount of concurrency to reconcile VerticaRestorePointQuery CRs")
}

// getEncoderConfig returns a concrete encoders configuration
func (o *OperatorConfig) getEncoderConfig(devMode bool) zapcore.EncoderConfig {
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	if !devMode {
		encoderConfig = zap.NewProductionEncoderConfig()
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}
	return encoderConfig
}

// getLogger is a wrapper that calls other functions
// to build a logger.
func (o *OperatorConfig) GetLogger() logr.Logger {
	encoderConfig := o.getEncoderConfig(o.DevMode)
	writes := []zapcore.WriteSyncer{}
	opts := []zap.Option{}
	lvl := zap.NewAtomicLevelAt(o.getZapcoreLevel())
	if o.FilePath != "" {
		w := o.getLogWriter()
		writes = append(writes, w)
	}
	if o.FilePath == "" || o.DevMode {
		writes = append(writes, zapcore.AddSync(os.Stdout))
	}
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(writes...),
		lvl,
	)
	opts = append(opts, o.getStackTrace())
	if !o.DevMode {
		// This enables sampling only in prod
		core = zapcore.NewSamplerWithOptions(core, time.Second, First, ThereAfter)
	}
	return zapr.NewLogger(zap.New(core, opts...))
}

// getLogWriter returns an io.writer (setting up rolling files) converted
// into a zapcore.WriteSyncer
func (o *OperatorConfig) getLogWriter() zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   o.FilePath,
		MaxSize:    o.MaxFileSize, // megabytes
		MaxBackups: o.MaxFileRotation,
		MaxAge:     o.MaxFileAge, // days
	}
	return zapcore.AddSync(lumberJackLogger)
}

// getZapcoreLevel takes the level as string and returns the corresponding
// zapcore.Level. If the string level is invalid, it returns the default
// level
func (o *OperatorConfig) getZapcoreLevel() zapcore.Level {
	var level = new(zapcore.Level)
	err := level.UnmarshalText([]byte(o.Logging.Level))
	if err != nil {
		log.Printf("unrecognized level, %s level will be used instead", DefaultLevel)
		return DefaultZapcoreLevel
	}
	return *level
}

// getStackTrace returns an option that configures
// the logger to record a stack strace.
func (o *OperatorConfig) getStackTrace() zap.Option {
	lvl := zapcore.ErrorLevel
	if o.DevMode {
		lvl = zapcore.WarnLevel
	}
	return zap.AddStacktrace(zapcore.LevelEnabler(lvl))
}
