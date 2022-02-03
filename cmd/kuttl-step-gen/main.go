/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/vertica/vertica-kubernetes/pkg/kstepgen"
)

const (
	StepCountArg = iota
	OutputDirArg
	NumPositionalArgs
)

func usage() {
	fmt.Printf("Usage: %s [OPTIONS] <stepCount> <outputDir>\n", os.Args[0])
	flag.PrintDefaults()
}

// nolint:gomnd
func main() {
	opts := kstepgen.MakeDefaultOptions()
	flag.Usage = usage
	flag.StringVar(&opts.ScriptDir, "script-dir", "../scripts",
		"The address relative to the output directory where the scripts path is.")
	flag.IntVar(&opts.MinPodsToKill, "min-pods-to-kill", 1,
		"The minimum number of pods to kill in a given test step")
	flag.IntVar(&opts.MaxPodsToKill, "max-pods-to-kill", 2,
		"The maximum number of pods to kill in a given test step")
	flag.IntVar(&opts.MinSleepTime, "min-sleep-time", 30,
		"The minimum sleep time when generating a sleep test step")
	flag.IntVar(&opts.MaxSleepTime, "max-sleep-time", 180,
		"The maximum sleep time when generating a sleep test step")
	flag.IntVar(&opts.MinSubclusters, "min-subclusters", 1,
		"The minimum number of subclusters to have in the CRD when doing a scaling test step")
	flag.IntVar(&opts.MaxSubclusters, "max-subclusters", 1,
		"The maximum number of subclusters to have in the CRD when doing a scaling test step")
	flag.IntVar(&opts.MinPods, "min-pods", 1,
		"The minimum number of pods, across all subclusters, to have in the CRD when doing a scaling test step")
	flag.IntVar(&opts.MaxPods, "max-pods", 3,
		"The maximum number of pods, across all subclusters, to have in the CRD when doing a scaling test step")
	flag.IntVar(&opts.SteadyStateTimeout, "steady-state-timeout", 900,
		"Amount of time to wait at the end of the iteration for the operator to get to a steady state")
	flag.Parse()

	if flag.NArg() < NumPositionalArgs {
		fmt.Println("Not enough positional arguments.")
		flag.Usage()
		os.Exit(1)
	}

	opts.StepCount, _ = strconv.Atoi(flag.Arg(StepCountArg))
	opts.OutputDir = flag.Arg(OutputDirArg)

	rand.Seed(time.Now().UTC().UnixNano())
	it := kstepgen.MakeIteration(opts)
	if err := it.CreateIteration(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
