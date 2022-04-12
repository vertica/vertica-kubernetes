/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
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
func getConfigMap(ctx context.Context, vrec *VerticaDBReconciler, vdb *vapi.VerticaDB,
	nm types.NamespacedName) (*corev1.ConfigMap, ctrl.Result, error) {
	cm := &corev1.ConfigMap{}
	res, err := getConfigMapOrSecret(ctx, vrec, vdb, nm, cm)
	return cm, res, err
}

// getConfigMapOrSecret is a generic function to fetch a ConfigMap or a Secret.
// It will handle logging an event if the configMap or secret is missing.
func getConfigMapOrSecret(ctx context.Context, vrec *VerticaDBReconciler, vdb *vapi.VerticaDB,
	nm types.NamespacedName, obj client.Object) (ctrl.Result, error) {
	if err := vrec.Client.Get(ctx, nm, obj); err != nil {
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
			vrec.EVRec.Eventf(vdb, corev1.EventTypeWarning, events.ObjectNotFound,
				"Could not find the %s '%s'", objType, nm)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{},
			fmt.Errorf("could not read the secret %s: %w", nm, err)
	}
	return ctrl.Result{}, nil
}
