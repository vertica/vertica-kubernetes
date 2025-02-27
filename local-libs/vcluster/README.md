# vcluster

[![Go Reference](https://pkg.go.dev/badge/github.com/vertica/vcluster.svg)](https://pkg.go.dev/github.com/vertica/vcluster)

This repository contains the vcluster-ops Go library and command-line 
interface to administer a Vertica cluster with a REST API. The REST API 
endpoints are exposed by the following services:
- Node Management Agent (NMA)
- Embedded HTTPS service

This CLI tool combines REST calls to provide a coherent Go interface so that 
you can perform the following administrator operations:
- Create a database
- Scale a cluster up and down
- Restart a cluster
- Stop a cluster
- Revive an Eon database

Historically, these operations were performed with [admintools](https://docs.vertica.com/latest/en/admin/using-admin-tools/admin-tools-reference/writing-admin-tools-scripts/).
However, admintools is not suitable for containerized environments because it
relies on SSH for communications and maintains a state file (admintools.conf)
on each Vertica host.

The [VerticaDB operator](https://github.com/vertica/vertica-kubernetes) uses
this library to perform database actions on Vertica on Kubernetes.

## Prerequisites
- [Go version 1.20](https://go.dev/doc/install) and higher


## Repository contents overview

```
vcluster/
├── commands
├── main.go
└── vclusterops
    ├── test_data
    ├── util
    ├── vlog
    └── vstruct
```

- `/vcluster` directory contains minimal code that sets up and invokes the
  entry point (main.go) for the vcluster CLI.
- `/commands`: Code for parsing command line options. These options are
  translated into arguments for the high-level functions in `/vclusterops`.
- `/vclusterops`: Library code for high-level operations such as CreateDB,
  StopDB, and StartDB. It also contains all code for executing the steps that
complete these functions. Code in this library should not depend on other
directories in this project.
  External projects import this library to build custom CLI tools.
- `/vclusterops/test_data`: Contains code and files for testing purposes. By
  convention, the go tool ignores the `/test_data` directory.
  This directory contains a YAML file that defines a simple three-node [Eon Mode cluster](https://docs.vertica.com/latest/en/architecture/eon-concepts/) for testing.
- `/vclusterops/util`: Code that is used by more than one library in this
  project and does not fit logically into an existing package.
- `/vclusterops/vlog`: Sets up a logging utility that writes to
  `/opt/vertica/log/vcluster.log`.
- `/vclusterops/vstruct`: Contains helper structs used by vcluster-ops.


## Usage
Each source file in `vclusterops/` contains a `V<Operation>Options` struct 
with option fields that you can set for that operation, and a `V<Operation>OptionsFactory` 
factory function that returns a struct with sensible option defaults. General
database and authentication options are available in `DatabaseOptions` in 
`vclusterops/vcluster_database_options.go`.

The following example imports the `vclusterops` library, and then calls 
functions from `vclusterops/create_db.go` to create a database:


```
import "github.com/vertica/vcluster/vclusterops"

// get default create_db options
opts := vclusterops.VCreateDatabaseOptionsFactory()

// set database options
opts.RawHosts = []string{"host1_ip", "host2_ip", "host3_ip"}
opts.DBName = "my_database"
*opts.ForceRemovalAtCreation = true
opts.CatalogPrefix = "/data"
opts.DataPrefix = "/data"

// set authentication options
opts.Key = "your_tls_key"
opts.Cert = "your_tls_cert"
opts.CaCert = "your_ca_cert"
*opts.UserName = "database_username"
opts.Password = "database_password"

// pass opts to VCreateDatabase function
vdb, err := vclusterops.VCreateDatabase(&opts)
if err != nil {
	// handle the error here
}
```

We can use similar way to set up and call other vcluster-ops commands.


## Licensing
vcluster is open source and is under the Apache 2.0 license. Please see 
`LICENSE` for details.
