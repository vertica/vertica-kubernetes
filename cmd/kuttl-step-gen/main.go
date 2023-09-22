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
	"flag"
	"fmt"
	"os"

	"github.com/vertica/vertica-kubernetes/pkg/kstepgen"
	yaml "gopkg.in/yaml.v2"
)

const (
	ConfigArg = iota
	NumPositionalArgs
)

func main() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	var locations kstepgen.Locations
	flag.StringVar(&locations.OutputDir, "output-dir", "",
		"The directory where the test steps will be generated.")
	flag.StringVar(&locations.ScriptsDir, "scripts-dir", "",
		"The relative directory to the output directory where the repository scripts directory is located.")
	flag.Parse()

	if flag.NArg() < NumPositionalArgs {
		fmt.Println("Not enough positional arguments.")
		flag.Usage()
		os.Exit(1)
	}

	configFileName := flag.Arg(ConfigArg)
	configRaw, err := os.ReadFile(configFileName)
	if err != nil {
		fmt.Printf("Failed to read config file %s: %s", configFileName, err.Error())
		os.Exit(1)
	}
	var config kstepgen.Config
	if err := yaml.Unmarshal(configRaw, &config); err != nil {
		fmt.Printf("Failed to parse config file %s: %s", configFileName, err.Error())
		os.Exit(1)
	}

	it := kstepgen.MakeIteration(&locations, &config)
	if err := it.CreateIteration(); err != nil {
		fmt.Printf("Failed to create iteration: %s", err.Error())
		os.Exit(1)
	}
}
