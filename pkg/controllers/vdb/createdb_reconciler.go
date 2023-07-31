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

package vdb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/license"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	vtypes "github.com/vertica/vertica-kubernetes/pkg/types"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// This is a file that we run with the create_db to run custome SQL. This is
	// passed with the --sql parameter when running create_db. This is no longer
	// used starting with versions defined in vapi.DBSetupConfigParameters.
	PostDBCreateSQLFile = "/home/dbadmin/post-db-create.sql"
)

// CreateDBReconciler will create a database if one wasn't created yet.
type CreateDBReconciler struct {
	VRec                *VerticaDBReconciler
	Log                 logr.Logger
	Vdb                 *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner             cmds.PodRunner
	PFacts              *PodFacts
	Dispatcher          vadmin.Dispatcher
	ConfigurationParams *vtypes.CiMap
}

// MakeCreateDBReconciler will build a CreateDBReconciler object
func MakeCreateDBReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts,
	dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &CreateDBReconciler{
		VRec:                vdbrecon,
		Log:                 log,
		Vdb:                 vdb,
		PRunner:             prunner,
		PFacts:              pfacts,
		Dispatcher:          dispatcher,
		ConfigurationParams: vtypes.MakeCiMap(),
	}
}

// Reconcile will ensure a DB exists and create one if it doesn't
func (c *CreateDBReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// Skip this reconciler entirely if the init policy is not to create the DB.
	if c.Vdb.Spec.InitPolicy != vapi.CommunalInitPolicyCreate &&
		c.Vdb.Spec.InitPolicy != vapi.CommunalInitPolicyCreateSkipPackageInstall {
		return ctrl.Result{}, nil
	}

	// The remaining create_db logic is driven from GenericDatabaseInitializer.
	// This exists to creation an abstraction that is common with revive_db.
	g := GenericDatabaseInitializer{
		initializer:         c,
		VRec:                c.VRec,
		Log:                 c.Log,
		Vdb:                 c.Vdb,
		PRunner:             c.PRunner,
		PFacts:              c.PFacts,
		ConfigurationParams: c.ConfigurationParams,
	}
	return g.checkAndRunInit(ctx)
}

// execCmd will do the actual execution of creating a database.
// This handles logging of necessary events.
func (c *CreateDBReconciler) execCmd(ctx context.Context, initiatorPod types.NamespacedName, hostList []string) (ctrl.Result, error) {
	opts, err := c.genOptions(ctx, initiatorPod, hostList)
	if err != nil {
		return ctrl.Result{}, err
	}
	c.VRec.Event(c.Vdb, corev1.EventTypeNormal, events.CreateDBStart, "Starting create database")
	start := time.Now()
	if res, err := c.Dispatcher.CreateDB(ctx, opts...); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	sc := c.getFirstPrimarySubcluster()
	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateDBSucceeded,
		"Successfully created database with subcluster '%s'. It took %s", sc.Name, time.Since(start))
	return ctrl.Result{}, nil
}

// preCmdSetup will generate the file we include with the create_db.
// This file runs any custom SQL for the create_db.
func (c *CreateDBReconciler) preCmdSetup(ctx context.Context, initiatorPod types.NamespacedName, podList []*PodFact) (ctrl.Result, error) {
	// If the communal path is a POSIX file path, we need to create the communal
	// path directory as the server won't create it. It handles that for other
	// communal types though.
	if c.Vdb.Spec.Communal.Path != "" && !c.Vdb.IsKnownCommunalPrefix() {
		// We intentionally skip any errors. If there is an error creating the
		// directory, this will manifest itself later when we attempt the
		// created. That error will have better reporting than if we were
		// handle it here.
		_, _, _ = c.PRunner.ExecInPod(ctx, initiatorPod, names.ServerContainer,
			"bash", "-c", fmt.Sprintf("mkdir -p %s", c.Vdb.GetCommunalPath()),
		)
	}

	// If setting encryptSpreadComm, we need to drive a restart of the vertica
	// pods immediately after database creation for the setting to take effect.
	if c.Vdb.Spec.EncryptSpreadComm != "" {
		cond := vapi.VerticaDBCondition{Type: vapi.VerticaRestartNeeded, Status: corev1.ConditionTrue}
		if err := vdbstatus.UpdateCondition(ctx, c.VRec.Client, c.Vdb, cond); err != nil {
			return ctrl.Result{}, err
		}
	}

	return c.generatePostDBCreateSQL(ctx, initiatorPod)
}

// generatePostDBCreateSQL is a function that creates a file with sql commands
// to be run immediately after the database create.
func (c *CreateDBReconciler) generatePostDBCreateSQL(ctx context.Context, initiatorPod types.NamespacedName) (ctrl.Result, error) {
	// On newer server versions we moved over the SQL to config parameters. So,
	// if we are on a new enough version we can skip this function entirely.
	vinf, ok := c.Vdb.MakeVersionInfo()
	if ok && vinf.IsEqualOrNewer(vapi.DBSetupConfigParametersMinVersion) {
		return ctrl.Result{}, nil
	}

	// We include SQL to rename the default subcluster to match the name of the
	// first subcluster in the spec -- any remaining subclusters will be added
	// by DBAddSubclusterReconciler.
	sc := c.getFirstPrimarySubcluster()
	var sb strings.Builder
	sb.WriteString("-- SQL that is run after the database is created\n")
	if c.Vdb.IsEON() {
		sb.WriteString(
			fmt.Sprintf(`alter subcluster default_subcluster rename to \"%s\";`, sc.Name),
		)
	}
	if c.Vdb.Spec.KSafety == vapi.KSafety0 {
		sb.WriteString("select set_preferred_ksafe(0);\n")
	}
	_, _, err := c.PRunner.ExecInPod(ctx, initiatorPod, names.ServerContainer,
		"bash", "-c", "cat > "+PostDBCreateSQLFile+"<<< \""+sb.String()+"\"",
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// postCmdCleanup will handle any cleanup action after initializing the database
func (c *CreateDBReconciler) postCmdCleanup(ctx context.Context) (ctrl.Result, error) {
	// If encryptSpreadComm was set we need to initiate a restart of the
	// cluster.  This is done in a separate reconciler.  We will requeue to
	// drive it.
	if c.Vdb.Spec.EncryptSpreadComm != "" {
		c.Log.Info("Requeue reconcile cycle to initiate restart of the server due to encryptSpreadComm setting")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// getPodList gets a list of all of the pods we are going to use with create db.
// If any pod is not found in the pod facts, it return false for the bool
// return value.
func (c *CreateDBReconciler) getPodList() ([]*PodFact, bool) {
	// We grab all pods from the first primary subcluster.  Pods for additional
	// subcluster are added through db_add_node.
	sc := c.getFirstPrimarySubcluster()
	podList := make([]*PodFact, 0, sc.Size)
	for i := int32(0); i < sc.Size; i++ {
		pn := names.GenPodName(c.Vdb, sc, i)
		pf, ok := c.PFacts.Detail[pn]
		// Bail out if one of the pods in the subcluster isn't found
		if !ok {
			return []*PodFact{}, false
		}
		podList = append(podList, pf)
	}
	// We need the podList to be ordered by its compat21 node number. This
	// ensures the assigned vnode number will match the compat21 node number.
	// admintools -t restart_db depends on this.
	sort.Slice(podList, func(i, j int) bool {
		return podList[i].compat21NodeName < podList[j].compat21NodeName
	})

	// Check if the shard/node ratio of the first subcluster is good
	c.VRec.checkShardToNodeRatio(c.Vdb, sc)

	// In case that kSafety == 0 (KSafety0), we only pick one pod from the first
	// primary subcluster. The remaining pods would be added with db_add_node.
	if c.Vdb.Spec.KSafety == vapi.KSafety0 {
		return podList[0:1], true
	}
	return podList, true
}

// findPodToRunInit will return a PodFact of the pod that should run the init
// command from
func (c *CreateDBReconciler) findPodToRunInit() (*PodFact, bool) {
	// Always return the first pod of the first primary subcluster. We do this
	// so that we can consistently pick the same pod if we have redo the create.
	sc := c.getFirstPrimarySubcluster()
	pf, ok := c.PFacts.Detail[names.GenPodName(c.Vdb, sc, 0)]
	return pf, ok
}

// getFirstPrimarySubcluster returns the first primary subcluster defined in the vdb
func (c *CreateDBReconciler) getFirstPrimarySubcluster() *vapi.Subcluster {
	sc := c.Vdb.GetFirstPrimarySubcluster()
	c.Log.Info("First primary subcluster selected for create_db", "sc", sc.Name)
	return sc
}

// genOptions will return the options to use for the create db command
func (c *CreateDBReconciler) genOptions(ctx context.Context, initiatorPod types.NamespacedName,
	hostList []string) ([]createdb.Option, error) {
	licPath, err := license.GetPath(ctx, c.VRec.Client, c.Vdb)
	if err != nil {
		return nil, err
	}

	opts := []createdb.Option{
		createdb.WithInitiator(initiatorPod),
		createdb.WithHosts(hostList),
		createdb.WithCatalogPath(c.Vdb.Spec.Local.GetCatalogPath()),
		createdb.WithDBName(c.Vdb.Spec.DBName),
		createdb.WithLicensePath(licPath),
		createdb.WithDepotPath(c.Vdb.Spec.Local.DepotPath),
		createdb.WithDataPath(c.Vdb.Spec.Local.DataPath),
	}

	vinf, ok := c.Vdb.MakeVersionInfo()
	if !ok || !vinf.IsEqualOrNewer(vapi.DBSetupConfigParametersMinVersion) {
		opts = append(opts, createdb.WithPostDBCreateSQLFile(PostDBCreateSQLFile))
	}

	// If a communal path is set, include all of the EON parameters.
	if c.Vdb.Spec.Communal.Path != "" {
		opts = append(opts,
			createdb.WithCommunalPath(c.Vdb.GetCommunalPath()),
			createdb.WithCommunalStorageParams(paths.AuthParmsFile),
			createdb.WithConfigurationParams(c.ConfigurationParams.GetMap()),
		)
	}

	if c.Vdb.Spec.ShardCount > 0 {
		opts = append(opts,
			createdb.WithShardCount(c.Vdb.Spec.ShardCount),
		)
	}

	if c.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyCreateSkipPackageInstall {
		vinf, ok := c.Vdb.MakeVersionInfo()
		if ok && vinf.IsEqualOrNewer(vapi.CreateDBSkipPackageInstallVersion) {
			opts = append(opts, createdb.WithSkipPackageInstall())
		}
	}
	return opts, nil
}
