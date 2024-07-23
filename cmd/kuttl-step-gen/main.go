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
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ghodss/yaml"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/vertica/vertica-kubernetes/pkg/kstepgen"
	"go.uber.org/zap"
)

const configPath = "/tmp/local-soak.cfg"

const configDir = "/tmp"

func setupLog() logr.Logger {
	cfg := zap.Config{
		Level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		Development: false,
		// Sampling is enabled at 100:100, meaning that after the first 100 log
		// entries with the same level and message in the same second, it will
		// log every 100th entry with the same level and message in the same second.
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         "console",
		EncoderConfig:    zap.NewDevelopmentEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}
	var err error
	zapLg, err := cfg.Build()
	if err != nil {
		fmt.Printf("Failed to setup the logger: %s", err.Error())
		os.Exit(1)
	}
	return zapr.NewLogger(zapLg)
}

func main() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var locations kstepgen.Locations
	flag.StringVar(&locations.OutputDir, "output-dir", "",
		"The directory where the test steps will be generated.")
	flag.StringVar(&locations.ScriptsDir, "scripts-dir", "",
		"The relative directory to the output directory where the repository scripts directory is located.")
	flag.Parse()

	if !filepath.IsAbs(configPath) {
		fmt.Printf("'%s' is not an absolute path", configPath)
		os.Exit(1)
	}
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Printf("Failed to read config file %s: %s", configPath, err.Error())
		os.Exit(1)
	}
	var config kstepgen.Config
	if err := yaml.Unmarshal(configRaw, &config); err != nil {
		fmt.Printf("Failed to parse config file %s: %s", configPath, err.Error())
		os.Exit(1)
	}

	it := kstepgen.MakeIteration(setupLog(), &locations, &config)
	if err := it.CreateIteration(); err != nil {
		fmt.Printf("Failed to create iteration: %s", err.Error())
		os.Exit(1)
	}
}
