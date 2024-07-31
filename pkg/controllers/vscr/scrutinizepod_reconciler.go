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

package vscr

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	"github.com/vertica/vertica-kubernetes/pkg/vscrstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ScrutinizeCmdArgs struct {
	hosts       []string
	username    string
	tarballName string
}

// ScrutinizePodReconciler will reconcile for the scrutinize
// pod creation
type ScrutinizePodReconciler struct {
	VRec    *VerticaScrutinizeReconciler
	Vscr    *v1beta1.VerticaScrutinize
	Log     logr.Logger
	Vdb     *v1.VerticaDB
	ScrArgs *ScrutinizeCmdArgs
}

// MakeScrutinizePodReconciler will build a ScrutinizePodReconciler object
func MakeScrutinizePodReconciler(r *VerticaScrutinizeReconciler, vscr *v1beta1.VerticaScrutinize,
	log logr.Logger) controllers.ReconcileActor {
	return &ScrutinizePodReconciler{
		VRec: r,
		Vscr: vscr,
		Log:  log.WithName("ScrutinizePodReconciler"),
	}
}

func (s *ScrutinizePodReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if ScrutinizeReady is false. This means that VerticaDB
	// was not found or is not configured for vclusterops
	if s.Vscr.IsStatusConditionFalse(v1beta1.ScrutinizeReady) {
		return ctrl.Result{}, nil
	}

	isSet := s.Vscr.IsStatusConditionTrue(v1beta1.ScrutinizePodCreated)
	if isSet {
		return ctrl.Result{}, nil
	}

	s.Vdb = &v1.VerticaDB{}
	nm := names.GenNamespacedName(s.Vscr, s.Vscr.Spec.VerticaDBName)
	if res, err := vk8s.FetchVDB(ctx, s.VRec, s.Vscr, nm, s.Vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// collect information from a VerticaDB.
	if res, err := s.collectInfoFromVdb(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{}, s.createPod(ctx)
}

// collectInfoFromVdb fetches data from the VerticaDB so that we can provide all of the parameters
// to the vcluster scrutinize CLI
func (s *ScrutinizePodReconciler) collectInfoFromVdb(ctx context.Context) (ctrl.Result, error) {
	finder := iter.MakeSubclusterFinder(s.VRec.Client, s.Vdb)
	pods, err := finder.FindPods(ctx, iter.FindExisting, v1.MainCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	hosts := s.getHostList(pods.Items)
	if len(hosts) == 0 {
		s.Log.Info("could not find any pod with NMA running, requeue reconciliation")
		return ctrl.Result{Requeue: true}, nil
	}
	s.ScrArgs = &ScrutinizeCmdArgs{}
	s.ScrArgs.hosts = hosts
	s.ScrArgs.username = s.Vdb.GetVerticaUser()

	return ctrl.Result{}, nil
}

// createPod creates the scrutinize pod
func (s *ScrutinizePodReconciler) createPod(ctx context.Context) error {
	s.ScrArgs.tarballName = generateScrutinizeID()
	pod := builder.BuildScrutinizePod(s.Vscr, s.Vdb, s.buildScrutinizeCmdArgs(s.Vdb, s.Vscr))
	s.Log.Info("Creating scrutinize pod", "Name", s.Vscr.ExtractNamespacedName())
	err := ctrl.SetControllerReference(s.Vscr, pod, s.VRec.Scheme)
	if err != nil {
		return err
	}
	err = s.VRec.Client.Create(ctx, pod)
	if err != nil {
		// we do not check if it returns an error because we are going
		// to return the pod creation failed error anyway
		_ = vscrstatus.UpdateState(ctx, s.VRec.Client, s.Vscr, "PodCreationFailed")
		return err
	}
	s.Log.Info("Scrutinize pod created successfully")
	stat := s.Vscr.Status.DeepCopy()
	stat.PodName = pod.Name
	stat.PodUID = pod.UID
	stat.State = "PodCreated"
	stat.Conditions = []metav1.Condition{*v1.MakeCondition(v1beta1.ScrutinizePodCreated, metav1.ConditionTrue, "PodCreated")}
	return vscrstatus.UpdateStatus(ctx, s.VRec.Client, s.Vscr, stat)
}

// getHostList returns the list of hosts that have NMA running
func (s *ScrutinizePodReconciler) getHostList(pods []corev1.Pod) []string {
	hosts := []string{}
	for i := range pods {
		pod := &pods[i]
		if vk8s.IsNMAContainerReady(pod) {
			hosts = append(hosts, pod.Status.PodIP)
		}
	}
	return hosts
}

// buildScrutinizeCmdArgs returns the arguments of vcluster scrutinize command
func (s *ScrutinizePodReconciler) buildScrutinizeCmdArgs(vdb *v1.VerticaDB, vscr *v1beta1.VerticaScrutinize) []string {
	cmd := []string{
		"--db-user", s.ScrArgs.username,
		"--hosts", strings.Join(s.ScrArgs.hosts, ","),
		"--log-path", paths.ScrutinizeLogFile,
		"--tarball-name", s.ScrArgs.tarballName,
	}

	// --log-age-hours cannot be set alongside the *-time options,
	// and if attempted, should issue an error indicating so.
	if vscr.Spec.LogAgeHours != 0 && (vscr.Spec.LogAgeOldestTime != "" || vscr.Spec.LogAgeNewestTime != "") {
		s.Log.Info("--log-age-hours cannot be set alongside the *-time options")
		return cmd
	}

	// In order to facilitate diagnosing less recent problems,
	// scrutinize should be able to collect an arbitrary time range of logs
	if vscr.Spec.LogAgeHours != 0 {
		cmd = append(cmd, "--log-age-hours", strconv.Itoa(vscr.Spec.LogAgeHours))
	} else {
		if vscr.Spec.LogAgeOldestTime != "" {
			cmd = append(cmd, "--log-age-oldest-time", vscr.Spec.LogAgeOldestTime)
		}
		if vscr.Spec.LogAgeNewestTime != "" {
			cmd = append(cmd, "--log-age-newest-time", vscr.Spec.LogAgeNewestTime)
		}
	}

	// if there is no password, we need to explicitly
	// set the password flag with empty string as value,
	// to still assume password as the authentication method
	if vdb.Spec.PasswordSecret == "" {
		cmd = append(cmd, "--password=")
	} else if secrets.IsK8sSecret(vdb.Spec.PasswordSecret) {
		// when the password secret is on k8s, we mount it into the
		// container and have scrutinize read the password from the mounted file
		cmd = append(cmd, "--password-file", paths.ScrutinizeDBPasswordFile)
	}

	return cmd
}

// generateScrutinizeID returns a string, with the format VerticaScrutinize.yyyymmddhhmmss,
// that will be used as the name of the tarball generated by scrutinize
func generateScrutinizeID() string {
	const idPrefix = "VerticaScrutinize."
	const timeFmt = "20060102150405" // using fixed reference time from pkg 'time'
	idSuffix := time.Now().Format(timeFmt)
	return idPrefix + idSuffix
}
