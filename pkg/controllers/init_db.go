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

package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type DatabaseInitializer interface {
	getPodList() ([]*PodFact, bool)
	genCmd(ctx context.Context, hostList []string) ([]string, error)
	execCmd(ctx context.Context, atPod types.NamespacedName, cmd []string) (ctrl.Result, error)
	preCmdSetup(ctx context.Context, atPod types.NamespacedName) error
}

type GenericDatabaseInitializer struct {
	initializer DatabaseInitializer
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	Vdb         *vapi.VerticaDB
	PRunner     cmds.PodRunner
	PFacts      *PodFacts
}

// checkAndRunInit will check if the database needs to be initialized and run init if applicable
func (g *GenericDatabaseInitializer) checkAndRunInit(ctx context.Context) (ctrl.Result, error) {
	if err := g.PFacts.Collect(ctx, g.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	if exists := g.PFacts.doesDBExist(); exists.IsFalse() {
		res, err := g.runInit(ctx)
		if err != nil || res.Requeue {
			return res, err
		}
	} else if exists.IsNone() {
		// Could not determine if DB didn't exist.  Missing state with some of the pods.
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

// runInit will physically setup the database.
// Depending on g.initializer, this will either do create_db or revive_db.
func (g *GenericDatabaseInitializer) runInit(ctx context.Context) (ctrl.Result, error) {
	atPodFact, ok := g.PFacts.findPodToRunAdmintools()
	if !ok {
		// Could not find a runable pod to run from.
		return ctrl.Result{Requeue: true}, nil
	}
	atPod := atPodFact.name

	if res, err := g.ConstructAuthParms(ctx, atPod); err != nil || res.Requeue {
		return res, err
	}
	if err := g.initializer.preCmdSetup(ctx, atPod); err != nil {
		return ctrl.Result{}, err
	}

	podList, ok := g.initializer.getPodList()
	if !ok {
		// Was not able to generate the pod list
		return ctrl.Result{Requeue: true}, nil
	}
	ok = g.checkPodList(podList)
	if !ok {
		g.Log.Info("Aborting reconiliation as not all of required pods are running")
		return ctrl.Result{Requeue: true}, nil
	}

	// Cleanup for any prior failed attempt.
	if err := g.cleanupLocalFilesInPods(ctx, podList); err != nil {
		return ctrl.Result{}, err
	}

	if err := changeDepotPermissions(ctx, g.Vdb, g.PRunner, podList); err != nil {
		return ctrl.Result{}, err
	}

	debugDumpAdmintoolsConf(ctx, g.PRunner, atPod)

	cmd, err := g.initializer.genCmd(ctx, getHostList(podList))
	if err != nil {
		return ctrl.Result{}, err
	}
	if res, err := g.initializer.execCmd(ctx, atPod, cmd); err != nil || res.Requeue {
		return res, err
	}

	debugDumpAdmintoolsConf(ctx, g.PRunner, atPod)

	cond := vapi.VerticaDBCondition{Type: vapi.DBInitialized, Status: corev1.ConditionTrue}
	if err := status.UpdateCondition(ctx, g.VRec.Client, g.Vdb, cond); err != nil {
		return ctrl.Result{}, err
	}

	if err := g.DestroyAuthParms(ctx, atPod); err != nil {
		// Destroying the auth parms is a best effort. If we fail to delete it,
		// the reconcile will continue on.
		g.Log.Info("failed to destroy auth parms, ignoring failure", "err", err)
	}

	// The DB has been initialized. We invalidate the cache now so that next
	// access will refresh with the new db state. A status reconciler will
	// follow this that will update the Vdb status about the db existence.
	g.PFacts.Invalidate()

	return ctrl.Result{}, nil
}

// getHostList will return a host list from the given pods
func getHostList(podList []*PodFact) []string {
	hostList := make([]string, 0, len(podList))
	for _, pod := range podList {
		hostList = append(hostList, pod.podIP)
	}
	return hostList
}

// checkPodList ensures all of the pods that we will use for the init call are running
func (g *GenericDatabaseInitializer) checkPodList(podList []*PodFact) bool {
	for _, pod := range podList {
		// Bail if find one of the pods isn't running
		if !pod.isPodRunning {
			return false
		}
	}
	return true
}

// cleanupLocalFilesInPods will go through each pod and ensure their local files are gone.
// This step is necessary because a failed create_db can leave old state around.
func (g *GenericDatabaseInitializer) cleanupLocalFilesInPods(ctx context.Context, podList []*PodFact) error {
	for _, pod := range podList {
		// Cleanup any local paths. This step is needed if an earlier create_db
		// fails -- admintools does not clean everything up.
		if err := cleanupLocalFiles(ctx, g.Vdb, g.PRunner, pod.name); err != nil {
			return err
		}
	}
	return nil
}

// ConstructAuthParms builds the s3 authentication parms and ensure it exists in the pod
func (g *GenericDatabaseInitializer) ConstructAuthParms(ctx context.Context, atPod types.NamespacedName) (ctrl.Result, error) {
	// Extract the auth from the credential secret.
	auth, res, err := g.getS3Auth(ctx)
	if err != nil || res.Requeue {
		return res, err
	}

	_, _, err = g.PRunner.ExecInPod(ctx, atPod, names.ServerContainer,
		"bash", "-c", "cat > "+paths.AuthParmsFile+"<<< '"+
			"awsauth = "+auth+"\n"+
			"awsendpoint = "+g.getS3Endpoint()+"\n"+
			"awsenablehttps = "+g.getEnableHTTPS()+"\n"+
			"'",
	)

	// We log an event for this error because it could be caused by bad values
	// in the creds.  If the value we get out of the secret has undisplayable
	// characters then we won't even be able to copy the file.
	if err != nil {
		g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.S3AuthParmsCopyFailed,
			"Failed to copy s3 auth parms to the pod '%s'", atPod)
	}
	return ctrl.Result{}, err
}

// DestroyAuthParms will remove the auth parms file that was created in the pod
func (g *GenericDatabaseInitializer) DestroyAuthParms(ctx context.Context, atPod types.NamespacedName) error {
	_, _, err := g.PRunner.ExecInPod(ctx, atPod, names.ServerContainer,
		"rm", paths.AuthParmsFile,
	)
	return err
}

// getS3Auth will return the access key and secret key.
// Value is returned in the format: <accessKey>:<secretKey>
func (g *GenericDatabaseInitializer) getS3Auth(ctx context.Context) (string, ctrl.Result, error) {
	secret := &corev1.Secret{}
	if err := g.VRec.Client.Get(ctx, names.GenCommunalCredSecretName(g.Vdb), secret); err != nil {
		if errors.IsNotFound(err) {
			g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.S3CredsNotFound,
				"Could not find the communal credential secret '%s'", g.Vdb.Spec.Communal.CredentialSecret)
			return "", ctrl.Result{Requeue: true}, nil
		}
		return "", ctrl.Result{}, fmt.Errorf("could not read the communal credential secret %s: %w", g.Vdb.Spec.Communal.CredentialSecret, err)
	}

	accessKey, ok := secret.Data[S3AccessKeyName]
	if !ok {
		g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.S3CredsWrongKey,
			"The communal credential secret '%s' does not have a key named '%s'", g.Vdb.Spec.Communal.CredentialSecret, S3AccessKeyName)
		return "", ctrl.Result{Requeue: true}, nil
	}

	secretKey, ok := secret.Data[S3SecretKeyName]
	if !ok {
		g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.S3CredsWrongKey,
			"The communal credential secret '%s' does not have a key named '%s'", g.Vdb.Spec.Communal.CredentialSecret, S3SecretKeyName)
		return "", ctrl.Result{Requeue: true}, nil
	}

	auth := fmt.Sprintf("%s:%s", strings.TrimSuffix(string(accessKey), "\n"),
		strings.TrimSuffix(string(secretKey), "\n"))
	return auth, ctrl.Result{}, nil
}

// getS3Endpoint get the s3 endpoint for inclusion in the auth files.
// Takes the endpoint from vdb and strips off the protocol.
func (g *GenericDatabaseInitializer) getS3Endpoint() string {
	prefix := []string{"https://", "http://"}
	for _, pref := range prefix {
		if i := strings.Index(g.Vdb.Spec.Communal.Endpoint, pref); i == 0 {
			return g.Vdb.Spec.Communal.Endpoint[len(pref):]
		}
	}
	return g.Vdb.Spec.Communal.Endpoint
}

// getEnableHTTPS will return "1" if connecting to https otherwise return "0"
func (g *GenericDatabaseInitializer) getEnableHTTPS() string {
	if strings.HasPrefix(g.Vdb.Spec.Communal.Endpoint, "https://") {
		return "1"
	}
	return "0"
}
