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
	"os"

	"github.com/vertica/vertica-kubernetes/pkg/vdbgen"
)

const (
	HostArg = iota
	DBNameArg
	NumPositionalArgs
	DefaultVerticaPort = 5433
)

func usage() {
	fmt.Printf("Usage: %s [OPTIONS] <host> <db>\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	opts := vdbgen.Options{}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flag.Usage = usage
	flag.StringVar(&opts.User, "user", "dbadmin",
		"The user to connect to the database with.  This user must have sufficient priviledges to inspect the database structure.")
	flag.StringVar(&opts.Password, "password", "",
		"The password for the --user option")
	flag.StringVar(&opts.TLSMode, "tlsmode", "none",
		"The TLS mode to use when connecting.  Available values are: none, server, and server-strict")
	flag.StringVar(&opts.VdbName, "name", "vert",
		"The name of the VerticaDB object it will create")
	flag.IntVar(&opts.Port, "port", DefaultVerticaPort,
		"The port number of the host we are connecting to")
	flag.BoolVar(&opts.IgnoreClusterLease, "ignore-cluster-lease", false,
		"Set the ignoreClusteLease option in the output manifest.  This option is dangerous as it can lead to data corruption and is "+
			"only intended for test purposes.")
	flag.StringVar(&opts.Image, "image", "",
		"The vertica image to use in the cluster.  If this is omitted, the default image is used when the manifest is applied.")
	flag.StringVar(&opts.LicenseFile, "license", "",
		"A path to a license that you want to use with the VerticaDB manifest.  This license is included in a secret that gets "+
			"printed out with the other manifests.  If omitted, no license is set in the VerticaDB.")
	flag.Parse()

	if flag.NArg() < NumPositionalArgs {
		fmt.Println("Not enough positional arguments.")
		flag.Usage()
		os.Exit(1)
	}

	opts.Host = flag.Arg(HostArg)
	opts.DBName = flag.Arg(DBNameArg)

	if err := vdbgen.Generate(os.Stdout, &vdbgen.DBGenerator{Opts: &opts}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
