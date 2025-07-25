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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getSecret is a generic function to get a secret and return its contents.  If
// the secret is not found, an event is logged and the reconciliation is
// requeued.
func getSecret(ctx context.Context, vrec *VerticaDBReconciler, vdb *vapi.VerticaDB,
	nm types.NamespacedName) (*corev1.Secret, ctrl.Result, error) {
	secret := &corev1.Secret{}
	res, err := getConfigMapOrSecret(ctx, vrec, vdb, nm, secret)
	return secret, res, err
}

// getConfigMap is like getSecret except that it works for configMap's.  It the
// configMap is not found, then the ctrl.Result returned will indicate a requeue
// is needed.
func getConfigMap(ctx context.Context, vrec config.ReconcilerInterface, vdb *vapi.VerticaDB,
	nm types.NamespacedName) (*corev1.ConfigMap, ctrl.Result, error) {
	cm := &corev1.ConfigMap{}
	res, err := getConfigMapOrSecret(ctx, vrec, vdb, nm, cm)
	return cm, res, err
}

// getConfigMapOrSecret is a generic function to fetch a ConfigMap or a Secret.
// It will handle logging an event if the configMap or secret is missing.
func getConfigMapOrSecret(ctx context.Context, vrec config.ReconcilerInterface, vdb *vapi.VerticaDB,
	nm types.NamespacedName, obj client.Object) (ctrl.Result, error) {
	if err := vrec.GetClient().Get(ctx, nm, obj); err != nil {
		if errors.IsNotFound(err) {
			objType := ""
			switch v := obj.(type) {
			default:
				objType = fmt.Sprintf("%T", v)
			case *corev1.Secret:
				objType = "Secret"
			case *corev1.ConfigMap:
				objType = "ConfigMap"
			}
			vrec.Eventf(vdb, corev1.EventTypeWarning, events.ObjectNotFound,
				"Could not find the %s '%s'", objType, nm)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{},
			fmt.Errorf("could not read the secret %s: %w", nm, err)
	}
	return ctrl.Result{}, nil
}

// recreateSts will drop then create the statefulset
func recreateSts(ctx context.Context, vrec config.ReconcilerInterface, curSts, expSts *appsv1.StatefulSet, vdb *vapi.VerticaDB) error {
	if err := vrec.GetClient().Delete(ctx, curSts); err != nil {
		return err
	}
	return createSts(ctx, vrec, expSts, vdb)
}

// createSts will create a new sts. It assumes the statefulset doesn't already exist.
func createSts(ctx context.Context, vrec config.ReconcilerInterface, expSts *appsv1.StatefulSet, vdb *vapi.VerticaDB) error {
	err := ctrl.SetControllerReference(vdb, expSts, vrec.GetClient().Scheme())
	if err != nil {
		return err
	}
	return vrec.GetClient().Create(ctx, expSts)
}

// createDep will create a new deployment. It assumes the deployment doesn't already exist.
func createDep(ctx context.Context, vrec config.ReconcilerInterface, vpDep *appsv1.Deployment, vdb *vapi.VerticaDB) error {
	err := ctrl.SetControllerReference(vdb, vpDep, vrec.GetClient().Scheme())
	if err != nil {
		return err
	}
	return vrec.GetClient().Create(ctx, vpDep)
}

// readSecret will read a single secret
func readSecret(vdb *vapi.VerticaDB, vrec config.ReconcilerInterface, k8sClient client.Client,
	log logr.Logger, ctx context.Context, secretName string) (secret map[string][]byte, res ctrl.Result, err error) {
	nmSecretName := types.NamespacedName{
		Name:      secretName,
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

	secretData, res, err := secretFetcher.FetchAllowRequeue(ctx, nmSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return nil, res, err
	}

	return secretData, res, err
}
