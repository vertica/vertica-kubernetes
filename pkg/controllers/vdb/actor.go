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
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ScaledownActor is an interface that handles a part of scale down, either
// db_remove_node or uninstall.
type ScaledownActor interface {
	GetClient() client.Client
	GetVDB() *vapi.VerticaDB
	CollectPFacts(ctx context.Context) error
}

// scaledownSubcluster is called to either remove nodes or call uninstall.
// This is a common function that is used by the DBRemoveNodeReconciler and
// UninstallReconciler. It will call a func (scaleDownFunc) for a range of pods
// that are to be scaled down.
func scaledownSubcluster(ctx context.Context, act ScaledownActor, sc *vapi.Subcluster,
	scaleDownFunc func(context.Context, *vapi.Subcluster, int32, int32) (ctrl.Result, error)) (ctrl.Result, error) {
	if sc == nil {
		return ctrl.Result{}, nil
	}
	sts := &appsv1.StatefulSet{}
	if err := act.GetClient().Get(ctx, names.GenStsName(act.GetVDB(), sc), sts); err != nil {
		// A non-existent statefulset is okay, as it might not have been created yet.
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if sts.Status.Replicas > sc.Size {
		if err := act.CollectPFacts(ctx); err != nil {
			return ctrl.Result{}, err
		}

		res, err := scaleDownFunc(ctx, sc, sc.Size, sts.Status.Replicas-1)
		if err != nil {
			return res, fmt.Errorf("failed to scale down nodes in subcluster %s: %w", sc.Name, err)
		}
		if verrors.IsReconcileAborted(res, err) {
			return res, nil
		}
	}

	return ctrl.Result{}, nil
}
