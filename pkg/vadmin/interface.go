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

package vadmin

import (
	"context"

	"github.com/go-logr/logr"
	vops "github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addnode"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addsc"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removenode"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removesc"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopdb"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Dispatcher interface {
	// CreateDB will create a brand new database. It assumes the communal
	// storage location is empty.
	CreateDB(ctx context.Context, opts ...createdb.Option) (ctrl.Result, error)

	// ReviveDB will initialize a database using a pre-populated communal path.
	ReviveDB(ctx context.Context, opts ...revivedb.Option) (ctrl.Result, error)

	// DescribeDB will read state information about the database in communal
	// storage and return it back to the caller.
	DescribeDB(ctx context.Context, opts ...describedb.Option) (string, ctrl.Result, error)

	// FetchNodeState will determine if the given set of nodes are considered UP
	// or DOWN in our consensous state. It returns a map of vnode to its node state.
	FetchNodeState(ctx context.Context, opts ...fetchnodestate.Option) (map[string]string, ctrl.Result, error)

	// ReIP will update the catalog on disk with new IPs for all of the nodes given.
	ReIP(ctx context.Context, opts ...reip.Option) (ctrl.Result, error)

	// StopDB will stop all the vertica hosts of a running cluster.
	StopDB(ctx context.Context, opts ...stopdb.Option) error

	// AddNode will add a new vertica node to the cluster. If add node fails due to
	// a license limit, the error will be of type addnode.LicenseLimitError.
	AddNode(ctx context.Context, opts ...addnode.Option) error

	// AddSubcluster will create a subcluster in the vertica cluster.
	AddSubcluster(ctx context.Context, opts ...addsc.Option) error

	// RemoveNode will remove an existng vertica node from the cluster.
	RemoveNode(ctx context.Context, opts ...removenode.Option) error

	// RemoveSubcluster will remove the given subcluster from the vertica cluster.
	RemoveSubcluster(ctx context.Context, opts ...removesc.Option) error

	// RestartNode will restart a subset of nodes. Use this when vertica has not
	// lost cluster quorum. The IP given for each vnode may not match the current IP
	// in the vertica catalogs.
	RestartNode(ctx context.Context, opts ...restartnode.Option) (ctrl.Result, error)

	// StartDB will start a subset of nodes. Use this when vertica has lost
	// cluster quorum. The IP given for each vnode *must* match the current IP
	// in the vertica catalog. If they aren't a call to ReIP is necessary.
	StartDB(ctx context.Context, opts ...startdb.Option) (ctrl.Result, error)
}

const (
	// Constant for an up node, this is taken from the STATE column in NODES table
	StateUp = "UP"
)

// Admintools is the legacy style of running admin commands. All commands are
// sent to a process that runs admintools. The output is then parsed out of the
// stdout/stderr output that is captured.
type Admintools struct {
	PRunner  cmds.PodRunner
	Log      logr.Logger
	EVWriter events.EVWriter
	VDB      *vapi.VerticaDB
	DevMode  bool // true to include verbose logging for some operations
}

// MakeAdmintools will create a dispatcher that uses admintools to call the
// admin commands.
func MakeAdmintools(log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner, evWriter events.EVWriter, devMode bool) Dispatcher {
	return Admintools{
		PRunner:  prunner,
		VDB:      vdb,
		Log:      log,
		EVWriter: evWriter,
		DevMode:  devMode,
	}
}

// VClusterOps is the new style of running admin commands. It makes use of the
// vclusterops library to perform all of the admin operations via RESTful
// interfaces.
type VClusterOps struct {
	Log    logr.Logger
	VDB    *vapi.VerticaDB
	Client client.Client
	VClusterProvider
	Password string
}

// MakeVClusterOps will create a dispatcher that uses the vclusterops library for admin commands.
func MakeVClusterOps(log logr.Logger, vdb *vapi.VerticaDB, cli client.Client, vopsi VClusterProvider, passwd string) Dispatcher {
	return &VClusterOps{
		Log:              log,
		VDB:              vdb,
		Client:           cli,
		VClusterProvider: vopsi,
		Password:         passwd,
	}
}

type HTTPSCerts struct {
	Key    string
	Cert   string
	CaCert string
}

// VClusterProvider is for mocking test
// We will have two concrete implementations for the interface
// 1. real implementation in vcluster-ops library 2. mock implementation for unit test
type VClusterProvider interface {
	VCreateDatabase(options *vops.VCreateDatabaseOptions) (vops.VCoordinationDatabase, error)
}
