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

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	AWSRegionParm          = "awsregion"
	GCloudRegionParm       = "GCSRegion"
	S3SseCustomerAlgorithm = "S3SseCustomerAlgorithm"
	S3ServerSideEncryption = "S3ServerSideEncryption"
	S3SseCustomerKey       = "S3SseCustomerKey"
	SseAlgorithmAES256     = "AES256"
	SseAlgorithmAWSKMS     = "aws:kms"
)

type DatabaseInitializer interface {
	getPodList() ([]*PodFact, bool)
	findPodToRunInit() (*PodFact, bool)
	execCmd(ctx context.Context, initiatorPod types.NamespacedName, hostList []string, podNames []types.NamespacedName) (ctrl.Result, error)
	preCmdSetup(ctx context.Context, initiatorPod types.NamespacedName, initiatorIP string, podList []*PodFact) (ctrl.Result, error)
	postCmdCleanup(ctx context.Context) (ctrl.Result, error)
}

type GenericDatabaseInitializer struct {
	initializer DatabaseInitializer
	PRunner     cmds.PodRunner
	PFacts      *PodFacts
	ConfigParamsGenerator
}

// checkAndRunInit will check if the database needs to be initialized and run init if applicable
func (g *GenericDatabaseInitializer) checkAndRunInit(ctx context.Context) (ctrl.Result, error) {
	if err := g.PFacts.Collect(ctx, g.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// redo the create/revive process if the database creation/revival fails
	// or create/revive the process if it doesn't fail
	isSet, err := g.Vdb.IsConditionSet(vapi.DBInitialized)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !isSet {
		res, err := g.runInit(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// runInit will physically setup the database.
// Depending on g.initializer, this will either do create_db or revive_db.
func (g *GenericDatabaseInitializer) runInit(ctx context.Context) (ctrl.Result, error) {
	podList, ok := g.initializer.getPodList()
	if !ok {
		// Was not able to generate the pod list
		return ctrl.Result{Requeue: true}, nil
	}
	ok = g.checkPodList(podList)
	if !ok {
		g.Log.Info("Aborting reconciliation as not all required pods are running")
		return ctrl.Result{Requeue: true}, nil
	}

	initPodFact, ok := g.initializer.findPodToRunInit()
	if !ok {
		// Could not find a runable pod to run from.
		return ctrl.Result{Requeue: true}, nil
	}
	initiatorPod := initPodFact.name
	initiatorIP := initPodFact.podIP

	res, err := g.ConstructConfigParms(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	if res, err := g.initializer.preCmdSetup(ctx, initiatorPod, initiatorIP, podList); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	host, postNames := getHostAndPodNameList(podList)
	if res, err := g.initializer.execCmd(ctx, initiatorPod, host, postNames); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	cond := vapi.VerticaDBCondition{Type: vapi.DBInitialized, Status: corev1.ConditionTrue}
	if err := vdbstatus.UpdateCondition(ctx, g.VRec.Client, g.Vdb, cond); err != nil {
		return ctrl.Result{}, err
	}

	// The DB has been initialized. We invalidate the cache now so that next
	// access will refresh with the new db state. A status reconciler will
	// follow this that will update the Vdb status about the db existence.
	g.PFacts.Invalidate()

	// Handle any post initialization actions
	return g.initializer.postCmdCleanup(ctx)
}

// checkPodList ensures all of the pods that we will use for the init call are running
func (g *GenericDatabaseInitializer) checkPodList(podList []*PodFact) bool {
	for _, pod := range podList {
		// Bail if:
		// - find one of the pods isn't running
		// - installer hasn't run yet for the pod (only if admintools
		//	 as install is skipped in vclusterops mode).
		// - doesn't have the annotations that we use in the k8s Vertica DC
		//   table. This has to be present before we start vertica to populate
		//   the DC table correctly.
		if !pod.isPodRunning || !pod.hasDCTableAnnotations {
			return false
		}
		// Skip the next check since there is no install state
		// for vclusterops
		if vmeta.UseVClusterOps(g.Vdb.Annotations) {
			continue
		}
		if !pod.isInstalled {
			return false
		}
	}
	return true
}
