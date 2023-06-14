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
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
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
}

// Admintools is the legacy style of running admin commands. All commands are
// sent to a process that runs admintools. The output is then parsed out of the
// stdout/stderr output that is captured.
type Admintools struct {
	PRunner cmds.PodRunner
	Log     logr.Logger
	EVRec   record.EventRecorder
	VDB     *vapi.VerticaDB
}

// VClusterOps is the new style of running admin commands. It makes use of the
// vclusterops library to perform all of the admin operations via RESTful
// interfaces.
type VClusterOps struct{}

// Fake is used for dependency injection during test. All operations run as no-ops.
// SPILLY - can we remove this?
type Fake struct{}
