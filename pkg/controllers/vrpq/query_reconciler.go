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

package vrpq

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"github.com/vertica/vertica-kubernetes/pkg/security"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	restorepointsquery "github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restorepoints"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	stateQuerying     = "Querying"
	stateSuccessQuery = "Query successful"
	PKKeySize         = 2048
)

type QueryReconciler struct {
	VRec *VerticaRestorePointsQueryReconciler
	Vrpq *vapi.VerticaRestorePointsQuery
	Log  logr.Logger
	config.ConfigParamsGenerator
	OpCfg    opcfg.OperatorConfig
	PFacts   *PodFacts
	PRunner  cmds.PodRunner
	Password string
}

func MakeRestorePointsQueryReconciler(r *VerticaRestorePointsQueryReconciler, vrpq *vapi.VerticaRestorePointsQuery,
	log logr.Logger, prunner cmds.PodRunner, pfacts *PodFacts, password string) controllers.ReconcileActor {
	return &QueryReconciler{
		VRec:     r,
		Vrpq:     vrpq,
		Log:      log.WithName("QueryReconciler"),
		PFacts:   pfacts,
		PRunner:  prunner,
		Password: password,
		ConfigParamsGenerator: config.ConfigParamsGenerator{
			VRec: r,
			Log:  log.WithName("QueryReconciler"),
		},
	}
}

func (q *QueryReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op if QueryComplete is true
	isSet := q.Vrpq.IsStatusConditionTrue(vapi.Querying)
	if isSet {
		return ctrl.Result{}, nil
	}
	// collect information from a VerticaDB.
	if res, err := q.collectInfoFromVdb(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	dispatcher := q.makeDispatcher(q.Log, q.Vdb, q.PRunner, q.Password)
	// Create a TLS secret for the NMA service
	err := q.generateCertsForNMA(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if e := q.PFacts.Collect(ctx, q.Vdb); e != nil {
		return ctrl.Result{}, e
	}
	pf, found := q.PFacts.findRunningPod()
	if !found {
		q.Log.Info("No pods running")
		return ctrl.Result{}, nil
	}
	hostList := q.PFacts.findExpectedNodeIps()

	opts := []restorepointsquery.Option{}
	// extract out the communal and config information to pass down to the vclusterops API.
	opts = append(opts,
		restorepointsquery.WithInitiator(pf.name, pf.podIP),
		restorepointsquery.WithHosts(hostList),
		restorepointsquery.WithCommunalPath(q.Vdb.GetCommunalPath()),
		restorepointsquery.WithConfigurationParams(q.ConfigurationParams.GetMap()),
	)
	return ctrl.Result{}, q.runListRestorePoints(ctx, req, dispatcher, opts)
}

// fetch the VerticaDB and collect access information to the communal storage for the VerticaRestorePointsQuery CR,
func (q *QueryReconciler) collectInfoFromVdb(ctx context.Context) (ctrl.Result, error) {
	vdb := &v1.VerticaDB{}
	res := ctrl.Result{}
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var e error
		if res, e = fetchVDB(ctx, q.VRec, q.Vrpq, vdb); verrors.IsReconcileAborted(res, e) {
			return e
		}
		q.Vdb = vdb
		// build communal storage params if there is not one
		if q.ConfigurationParams == nil {
			res, e = q.ConstructConfigParms(ctx)
			if verrors.IsReconcileAborted(res, e) {
				return e
			}
		}
		return nil
	})

	return res, err
}

// runListRestorePoints will update the status condition before and after calling
// list restore points api
// Temporarily, runListRestorePoints will not call the ListRestorePoints API
// since the dispatcher is not set up yet
func (q *QueryReconciler) runListRestorePoints(ctx context.Context, _ *ctrl.Request, dispatcher vadmin.Dispatcher,
	opts []restorepointsquery.Option) error {
	// set Querying status condition and state prior to calling vclusterops API
	err := vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionTrue, "Started"), stateQuerying)
	if err != nil {
		return err
	}

	// API should be called to proceed here
	// If we receive a failure result from the API, a state message and condition need to be updated
	q.VRec.Eventf(q.Vdb, corev1.EventTypeNormal, events.ListRestorePointsStarted,
		"Starting list restore points")
	start := time.Now()
	if res, errRun := dispatcher.ListRestorePoints(ctx, opts...); verrors.IsReconcileAborted(res, errRun) {
		return errRun
	}
	q.VRec.Eventf(q.Vdb, corev1.EventTypeNormal, events.CreateDBSucceeded,
		"Successfully listed restore point with database '%s'. It took %s", q.Vdb.Spec.DBName, time.Since(start))

	// clear Querying status condition
	err = vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionFalse, "Completed"), stateQuerying)
	if err != nil {
		return err
	}

	// set the QueryComplete if the vclusterops API succeeded
	return vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.QueryComplete, metav1.ConditionTrue, "Completed"), stateSuccessQuery)
}

// makeDispatcher will create a Dispatcher object based on the feature flags set.
func (q *QueryReconciler) makeDispatcher(log logr.Logger, vdb *v1.VerticaDB, prunner cmds.PodRunner,
	passwd string) vadmin.Dispatcher {
	if vmeta.UseVClusterOps(vdb.Annotations) {
		return vadmin.MakeVClusterOps(log, vdb, q.VRec.GetClient(), passwd, q.VRec, vadmin.SetupVClusterOps)
	}
	return vadmin.MakeAdmintools(log, vdb, prunner, q.VRec, q.OpCfg.DevMode)
}

// getDNSNames returns the DNS names to include in the certificate that we generate
func (q *QueryReconciler) getDNSNames() []string {
	return []string{
		fmt.Sprintf("*.%s.svc", q.Vdb.Namespace),
		fmt.Sprintf("*.%s.svc.cluster.local", q.Vdb.Namespace),
	}
}

func (q *QueryReconciler) createSecret(ctx context.Context, cert, caCert security.Certificate) (*corev1.Secret, error) {
	isController := true
	blockOwnerDeletion := false
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   q.Vdb.Namespace,
			Annotations: builder.MakeAnnotationsForObject(q.Vdb),
			Labels:      builder.MakeCommonLabels(q.Vdb, nil, false),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         vapi.GroupVersion.String(),
					Kind:               vapi.VerticaDBKind,
					Name:               q.Vdb.Name,
					UID:                q.Vdb.GetUID(),
					Controller:         &isController,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey:   cert.TLSKey(),
			corev1.TLSCertKey:         cert.TLSCrt(),
			paths.HTTPServerCACrtName: caCert.TLSCrt(),
		},
	}
	// Either generate a name or use the one already present in the vdb. Using
	// the name already present is the case where the name was filled in but the
	// secret didn't exist.
	if q.Vdb.Spec.NMATLSSecret == "" {
		secret.GenerateName = fmt.Sprintf("%s-nma-tls-", q.Vdb.Name)
	} else {
		secret.Name = q.Vdb.Spec.NMATLSSecret
	}
	err := q.VRec.Client.Create(ctx, &secret)
	return &secret, err
}

func (q *QueryReconciler) setSecretNameInVDB(ctx context.Context, secretName string) error {
	nm := q.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest in case we are in the retry loop
		if err := q.VRec.Client.Get(ctx, nm, q.Vdb); err != nil {
			return err
		}
		q.Vdb.Spec.NMATLSSecret = secretName
		return q.VRec.Client.Update(ctx, q.Vdb)
	})
}

func (q *QueryReconciler) generateCertsForNMA(ctx context.Context) error {
	if q.Vdb.Spec.NMATLSSecret != "" {
		// As a convenience we will regenerate the secret using the same name. But
		// only do this if it is a k8s secret. We skip if there is a path reference
		// for a different secret store.
		if !secrets.IsK8sSecret(q.Vdb.Spec.NMATLSSecret) {
			q.Log.Info("nmaTLSSecret is set but uses a path reference that isn't for k8s.")
			return nil
		}
		nm := names.GenNamespacedName(q.Vdb, q.Vdb.Spec.NMATLSSecret)
		secret := corev1.Secret{}
		err := q.VRec.Client.Get(ctx, nm, &secret)
		if errors.IsNotFound(err) {
			q.Log.Info("nmaTLSSecret is set but doesn't exist. Will recreate the secret.", "name", nm)
		} else if err != nil {
			return fmt.Errorf("failed while attempting to read the tls secret %s: %w", q.Vdb.Spec.NMATLSSecret, err)
		} else {
			// Secret is filled in and exists. We can exit.
			return nil
		}
	}
	caCert, err := security.NewSelfSignedCACertificate(PKKeySize)
	if err != nil {
		return err
	}
	cert, err := security.NewCertificate(caCert, PKKeySize, "dbadmin", q.getDNSNames())
	if err != nil {
		return err
	}
	secret, err := q.createSecret(ctx, cert, caCert)
	if err != nil {
		return err
	}
	err = q.setSecretNameInVDB(ctx, secret.ObjectMeta.Name)
	if err != nil {
		return err
	}
	return nil
}
