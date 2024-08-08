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

package vadmin

import (
	"context"

	"github.com/go-logr/logr"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addnode"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addsc"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/altersc"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodedetails"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/getconfigparameter"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/installpackages"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/promotesandboxtomain"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removenode"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removesc"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/renamesc"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstart"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/sandboxsc"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/setconfigparameter"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/showrestorepoints"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopdb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/unsandboxsc"
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

	// ShowRestorePoints will list existing restore points in a database
	ShowRestorePoints(ctx context.Context, opts ...showrestorepoints.Option) ([]vops.RestorePoint, error)

	// InstallPackages will install all packages under /opt/vertica/packages
	// where Autoinstall is marked true.
	InstallPackages(ctx context.Context, opts ...installpackages.Option) (*vops.InstallPackageStatus, error)

	// ReplicateDB will start replicating data and metadata of an Eon cluster to another
	ReplicateDB(ctx context.Context, opts ...replicationstart.Option) (ctrl.Result, error)

	// FetchNodeDetails will return details for a node, including its state, sandbox, and storage locations
	FetchNodeDetails(ctx context.Context, opts ...fetchnodedetails.Option) (vops.NodeDetails, error)

	// SandboxSubcluster will add a subcluster in a sandbox of the database
	SandboxSubcluster(ctx context.Context, opts ...sandboxsc.Option) error

	// PromoteSandboxToMain will convert local sandbox to main cluster
	PromoteSandboxToMain(ctx context.Context, opts ...promotesandboxtomain.Option) error

	// UnsandboxSubcluster will move a subcluster from a sandbox to main cluster
	UnsandboxSubcluster(ctx context.Context, opts ...unsandboxsc.Option) error

	AlterSubclusterType(ctx context.Context, opts ...altersc.Option) error

	// SetConfigurationParameter will set a config parameter to a certain value at a certain level in a given cluster
	SetConfigurationParameter(ctx context.Context, opts ...setconfigparameter.Option) error

	// GetConfigurationParameter will get the value of a config parameter at a certain level in a given cluster
	GetConfigurationParameter(ctx context.Context, opts ...getconfigparameter.Option) (string, error)

	// RenameSubcluster will rename a subcluster in main cluster
	RenameSubcluster(ctx context.Context, opts ...renamesc.Option) error
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
}

// MakeAdmintools will create a dispatcher that uses admintools to call the
// admin commands.
func MakeAdmintools(log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner, evWriter events.EVWriter) Dispatcher {
	return &Admintools{
		PRunner:  prunner,
		VDB:      vdb,
		Log:      log,
		EVWriter: evWriter,
	}
}

// VClusterOps is the new style of running admin commands. It makes use of the
// vclusterops library to perform all of the admin operations via RESTful
// interfaces.
type VClusterOps struct {
	BaseLog  logr.Logger // The base logger that all log objects are derived from
	Log      logr.Logger // A copy of the current log that is currently in use in the vclusterops package
	VDB      *vapi.VerticaDB
	Client   client.Client
	Password string
	EVWriter events.EVWriter
	VClusterProvider
	// Setup function for VClusterProvider and Log in this struct
	APISetupFunc func(log logr.Logger, apiName string) (VClusterProvider, logr.Logger)
}

// MakeVClusterOps will create a dispatcher that uses the vclusterops library for admin commands.
func MakeVClusterOps(log logr.Logger, vdb *vapi.VerticaDB, cli client.Client,
	passwd string, evWriter events.EVWriter,
	apiSetupFunc func(logr.Logger, string) (VClusterProvider, logr.Logger)) Dispatcher {
	return &VClusterOps{
		BaseLog:          log,
		VDB:              vdb,
		Client:           cli,
		Password:         passwd,
		EVWriter:         evWriter,
		APISetupFunc:     apiSetupFunc,
		VClusterProvider: nil, // Setup via the APISetupFunc before each API call
	}
}

// SetupVClusterOps will provide a VClusterProvider that uses the *real*
// vclusterops package. This meant to be called ahead of each API to setup a
// custom logger for the API call. This function pointer is stored in
// APISetupFunc and is called via setupForAPICall.
func SetupVClusterOps(log logr.Logger, apiName string) (VClusterProvider, logr.Logger) {
	// We use a function to construct the VClusterProvider. This is called
	// ahead of each API rather than once so that we can setup a custom
	// logger for each API call.
	apiLog := log.WithName(apiName)
	return &vops.VClusterCommands{
			VClusterCommandsLogger: vops.VClusterCommandsLogger{
				Log: vlog.Printer{
					Log:           apiLog,
					LogToFileOnly: false,
				},
			},
		},
		apiLog
}

// setupForAPICall will setup the vcluster provider ahead of an API call. This
// must be called before each API call. Callers should call the tear down
// function (tearDownForAPICall) with defer.
//
//	func foo() {
//	    setupForAPICall("VCreateDatabase")
//	    defer tearDownForAPICall()
//	    ...
//	    v.VCreateDatabase(..)
//	}
func (v *VClusterOps) setupForAPICall(apiName string) {
	v.VClusterProvider, v.Log = v.APISetupFunc(v.BaseLog, apiName)
}

// tearDownForAPICall will cleanup from the setupForAPICall. This function
// should be called with defer immediately after calling setupForAPICall.
func (v *VClusterOps) tearDownForAPICall() {
	v.VClusterProvider = nil
}

type HTTPSCerts struct {
	Key    string
	Cert   string
	CaCert string
}

// VClusterProvider is for mocking test
// We will have two concrete implementations for the interface
//  1. real implementation in vcluster-ops library
//  2. mock implementation for unit test
type VClusterProvider interface {
	VCreateDatabase(options *vops.VCreateDatabaseOptions) (vops.VCoordinationDatabase, error)
	VStopDatabase(options *vops.VStopDatabaseOptions) error
	VStartDatabase(options *vops.VStartDatabaseOptions) (*vops.VCoordinationDatabase, error)
	VReviveDatabase(options *vops.VReviveDatabaseOptions) (string, *vops.VCoordinationDatabase, error)
	VFetchNodeState(options *vops.VFetchNodeStateOptions) ([]vops.NodeInfo, error)
	VAddSubcluster(options *vops.VAddSubclusterOptions) error
	VRemoveSubcluster(options *vops.VRemoveScOptions) (vops.VCoordinationDatabase, error)
	VAddNode(options *vops.VAddNodeOptions) (vops.VCoordinationDatabase, error)
	VRemoveNode(options *vops.VRemoveNodeOptions) (vops.VCoordinationDatabase, error)
	VReIP(options *vops.VReIPOptions) error
	VStartNodes(options *vops.VStartNodesOptions) error
	VShowRestorePoints(options *vops.VShowRestorePointsOptions) ([]vops.RestorePoint, error)
	VInstallPackages(options *vops.VInstallPackagesOptions) (*vops.InstallPackageStatus, error)
	VReplicateDatabase(options *vops.VReplicationDatabaseOptions) error
	VFetchNodesDetails(options *vops.VFetchNodesDetailsOptions) (vops.NodesDetails, error)
	VPromoteSandboxToMain(options *vops.VPromoteSandboxToMainOptions) error
	VSandbox(options *vops.VSandboxOptions) error
	VUnsandbox(options *vops.VUnsandboxOptions) error
	VAlterSubclusterType(options *vops.VAlterSubclusterTypeOptions) error
	VSetConfigurationParameters(options *vops.VSetConfigurationParameterOptions) error
	VGetConfigurationParameters(options *vops.VGetConfigurationParameterOptions) (string, error)
	VRenameSubcluster(options *vops.VRenameSubclusterOptions) error
}
