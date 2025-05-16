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

package vdb

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	PredefinedTLSConfigName = "k8s_tls_builtin_auth"
)

// TLSConfigReconciler will turn on the tls config when users request it
type TLSConfigReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	PRunner    cmds.PodRunner
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeTLSConfigReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &TLSConfigReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("TLSConfigReconciler"),
		Dispatcher: dispatcher,
		PRunner:    prunner,
		Pfacts:     pfacts,
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSConfigReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	h.Log.Info("in tls config reconcile 1")
	if !meta.SetupTLSConfig(h.Vdb.Annotations) || h.Vdb.IsCertRotationEnabled() && len(h.Vdb.Status.SecretRefs) != 0 ||
		!h.Vdb.IsStatusConditionTrue(vapi.DBInitialized) || h.Vdb.IsStatusConditionTrue(vapi.UpgradeInProgress) ||
		h.Vdb.IsStatusConditionTrue(vapi.VerticaRestartNeeded) {
		return ctrl.Result{}, nil
	}

	h.Log.Info("entry condition, cert rotate enabled ? " + strconv.FormatBool(h.Vdb.IsCertRotationEnabled()) +
		", num of status secrets - " + strconv.Itoa(len(h.Vdb.Status.SecretRefs)) + ", is db initialized ? " +
		strconv.FormatBool(h.Vdb.IsStatusConditionTrue(vapi.DBInitialized)) + ", setup tls - " +
		strconv.FormatBool(meta.SetupTLSConfig(h.Vdb.Annotations)))
	h.Log.Info("tls enabled, start to set up tls config")
	err := h.Pfacts.Collect(ctx, h.Vdb)
	if err != nil {
		h.Log.Error(err, "failed to collect pfacts to set up tls. skip current loop")
		return ctrl.Result{}, nil
	}
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to set up tls config. skip current loop.")
		return ctrl.Result{}, nil
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSConfigurationStarted,
		"Starting to configure TLS")

	h.Log.Info("will restart nma before setting up tls config")
	res, err := h.restartNMA(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	h.Log.Info("restarted nma before setting up tls config")
	h.Log.Info("generate SQL to set up TLS")
	err = h.runTLSDDL(ctx, initiatorPod)
	if err != nil {
		return ctrl.Result{}, err
	}
	h.Log.Info("executed SQL to set up TLS")
	err = h.updateStatus(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	chgs := vk8s.MetaChanges{
		NewAnnotations: map[string]string{
			meta.EnableTLSCertsRotationAnnotation: "true",
			meta.SetupTLSConfigAnnotation:         "false",
		},
	}
	if _, err := vk8s.MetaUpdate(ctx, h.VRec.Client, h.Vdb.ExtractNamespacedName(), h.Vdb, chgs); err != nil {
		h.Log.Error(err, "failed to update tls annotations after setting up TLS")
		return ctrl.Result{}, err
	}
	h.Log.Info("saved TLS modes into status")
	h.Log.Info("TLS DDLs executed and TLS configured for the existing vdb")
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSConfigurationSucceeded,
		"Successfully configured TLS")
	return ctrl.Result{}, nil
}

func (h *TLSConfigReconciler) restartNMA(ctx context.Context) (ctrl.Result, error) {
	currentSecretData, res, err := h.readSecret(h.Vdb, h.VRec, h.VRec.GetClient(), h.Log, ctx,
		h.Vdb.Spec.NMATLSSecret)
	if verrors.IsReconcileAborted(res, err) {
		h.Log.Error(err, "failed to read secret to set up TLS config")
		return res, err
	}
	hosts := []string{}
	for _, detail := range h.Pfacts.Detail {
		hosts = append(hosts, detail.GetPodIP())
	}
	opts := []rotatenmacerts.Option{
		rotatenmacerts.WithKey(string(currentSecretData[corev1.TLSPrivateKeyKey])),
		rotatenmacerts.WithCert(string(currentSecretData[corev1.TLSCertKey])),
		rotatenmacerts.WithCaCert(string(currentSecretData[corev1.ServiceAccountRootCAKey])),
		rotatenmacerts.WithHosts(hosts),
	}
	err = h.Dispatcher.RotateNMACerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to set nma cert to "+h.Vdb.Spec.NMATLSSecret)
	}
	return ctrl.Result{}, err
}

func (h *TLSConfigReconciler) runTLSDDL(ctx context.Context, initiatorPod *podfacts.PodFact) error {
	var sb strings.Builder
	h.generateKubernetesTLSSQL(&sb)
	sb.WriteString(`select sync_catalog();`)
	cmd := "cat > " + PostDBCreateSQLFileVclusterOps + "<<< " + escapeForBash(sb.String())
	h.Log.Info("SQL to be executed after db creation: " + sb.String())
	_, _, err := h.PRunner.ExecInPod(ctx, initiatorPod.GetName(), names.ServerContainer,
		"bash", "-c", cmd,
	)
	if err != nil {
		h.Log.Error(err, "failed to prepare the SQL scripts to set up TLS")
		return err
	}
	setupCmd := []string{
		"-f", PostDBCreateSQLFileVclusterOps,
	}
	_, stderr, err2 := h.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, setupCmd...)
	if err2 != nil || strings.Contains(stderr, "Error") {
		h.Log.Error(err2, "failed to execute TLS DDLs,  stderr - "+stderr)
		return err2
	}
	return nil
}
func (h *TLSConfigReconciler) updateStatus(ctx context.Context) error {
	sec := vapi.MakeNMATLSSecretRef(h.Vdb.Spec.NMATLSSecret)
	if err1 := vdbstatus.UpdateSecretRef(ctx, h.VRec.GetClient(), h.Vdb, sec); err1 != nil {
		return err1
	}
	clientSec := vapi.MakeClientServerTLSSecretRef(h.Vdb.Spec.ClientServerTLSSecret)
	if err3 := vdbstatus.UpdateSecretRef(ctx, h.VRec.GetClient(), h.Vdb, clientSec); err3 != nil {
		return err3
	}
	sRefs := []*vapi.SecretRef{
		sec, clientSec,
	}
	if err2 := vdbstatus.UpdateSecretRefs(ctx, h.VRec.GetClient(), h.Vdb, sRefs); err2 != nil {
		return err2
	}
	h.Log.Info("saved secrets into status")
	httpsTLSMode := vapi.MakeNMATLSMode(h.Vdb.Spec.HTTPSTLSMode)
	clientTLSMode := vapi.MakeClientServerTLSMode(h.Vdb.Spec.ClientServerTLSMode)
	err := vdbstatus.UpdateTLSModes(ctx, h.VRec.GetClient(), h.Vdb, []*vapi.TLSMode{httpsTLSMode, clientTLSMode})
	if err != nil {
		h.Log.Error(err, "failed to update tls mode when setting up TLS")
		return err
	}
	return nil
}

//nolint:dupl
func (h *TLSConfigReconciler) generateKubernetesTLSSQL(sb *strings.Builder) {
	fmt.Fprintf(sb, "CREATE OR REPLACE LIBRARY public.KubernetesLib AS ")
	fmt.Fprintf(sb, "'/opt/vertica/packages/kubernetes/lib/libkubernetes.so';\n")
	fmt.Fprintf(sb, "CREATE OR REPLACE SECRETMANAGER KubernetesSecretManager AS LANGUAGE 'C++' ")
	fmt.Fprintf(sb, "NAME 'KubernetesSecretManagerFactory' LIBRARY KubernetesLib;\n")

	fmt.Fprintf(sb, "DROP KEY IF EXISTS https_key_0;\n")
	fmt.Fprintf(sb, "DROP CERTIFICATE IF EXISTS https_cert_0;\n")
	fmt.Fprintf(sb, "DROP CERTIFICATE IF EXISTS https_ca_cert_0;\n")

	fmt.Fprintf(sb, "CREATE KEY https_key_0 TYPE 'rsa' SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}';\n",
		h.Vdb.Spec.NMATLSSecret, corev1.TLSPrivateKeyKey, h.Vdb.ObjectMeta.Namespace)

	fmt.Fprintf(sb, "CREATE CA CERTIFICATE https_ca_cert_0 SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}';\n",
		h.Vdb.Spec.NMATLSSecret, paths.HTTPServerCACrtName, h.Vdb.ObjectMeta.Namespace)

	fmt.Fprintf(sb, "CREATE CERTIFICATE https_cert_0 SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}' ",
		h.Vdb.Spec.NMATLSSecret, corev1.TLSCertKey, h.Vdb.ObjectMeta.Namespace)
	fmt.Fprintf(sb, "SIGNED BY https_ca_cert_0 KEY https_key_0;\n")

	fmt.Fprintf(sb, "DROP KEY IF EXISTS server_key;\n")
	fmt.Fprintf(sb, "DROP CERTIFICATE IF EXISTS server_cert;\n")
	fmt.Fprintf(sb, "DROP CERTIFICATE IF EXISTS server_ca_cert;\n")

	fmt.Fprintf(sb, "CREATE KEY server_key TYPE 'rsa' SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}';\n",
		h.Vdb.Spec.ClientServerTLSSecret, corev1.TLSPrivateKeyKey, h.Vdb.ObjectMeta.Namespace)

	fmt.Fprintf(sb, "CREATE CA CERTIFICATE server_ca_cert SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}';\n",
		h.Vdb.Spec.ClientServerTLSSecret, paths.HTTPServerCACrtName, h.Vdb.ObjectMeta.Namespace)

	fmt.Fprintf(sb, "CREATE CERTIFICATE server_cert SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}' ",
		h.Vdb.Spec.ClientServerTLSSecret, corev1.TLSCertKey, h.Vdb.ObjectMeta.Namespace)
	fmt.Fprintf(sb, "SIGNED BY server_ca_cert KEY server_key;\n")

	fmt.Fprintf(sb, "ALTER TLS CONFIGURATION server CERTIFICATE server_cert ADD CA CERTIFICATES ")
	fmt.Fprintf(sb, "server_ca_cert TLSMODE '%s';\n", h.Vdb.Spec.ClientServerTLSMode)
	fmt.Fprintf(sb, "ALTER TLS CONFIGURATION https CERTIFICATE https_cert_0 ADD CA CERTIFICATES ")
	fmt.Fprintf(sb, "https_ca_cert_0 TLSMODE 'TRY_VERIFY';\n")
	fmt.Fprintf(sb, "ALTER TLS CONFIGURATION https CERTIFICATE https_cert_0 REMOVE CA CERTIFICATES ")
	fmt.Fprintf(sb, "httpServerRootca;\n")
	fmt.Fprintf(sb, "ALTER TLS CONFIGURATION server CERTIFICATE server_cert REMOVE CA CERTIFICATES ")
	fmt.Fprintf(sb, "httpServerRootca;\n")
	fmt.Fprintf(sb, "CREATE AUTHENTICATION k8s_tls_builtin_auth METHOD 'tls' HOST TLS '0.0.0.0/0' FALLTHROUGH;\n")
	fmt.Fprintf(sb, "GRANT AUTHENTICATION k8s_tls_builtin_auth TO %s;\n", h.Vdb.GetVerticaUser())
}

func (h *TLSConfigReconciler) readSecret(vdb *vapi.VerticaDB, vrec config.ReconcilerInterface, k8sClient client.Client,
	log logr.Logger, ctx context.Context, currentSecretName string) (currentSecretData map[string][]byte, res ctrl.Result, err error) {
	nmCurrentSecretName := types.NamespacedName{
		Name:      currentSecretName,
		Namespace: vdb.GetNamespace(),
	}

	evWriter := events.Writer{
		Log:   log,
		EVRec: vrec.GetEventRecorder(),
	}
	secretFetcher := &cloud.SecretFetcher{
		Client:   k8sClient,
		Log:      log,
		EVWriter: evWriter,
		Obj:      vdb,
	}

	currentSecretData, res, err = secretFetcher.FetchAllowRequeue(ctx, nmCurrentSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return nil, res, err
	}
	return currentSecretData, res, err
}
