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

package controllers

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ApplyMethodType string

const (
	AddNodeApplyMethod       ApplyMethodType = "Add"
	PodRescheduleApplyMethod ApplyMethodType = "PodReschedule"
	DelNodeApplyMethod       ApplyMethodType = "Remove"
)

type SubscriptionLabelReconciler struct {
	VRec        *VerticaDBReconciler
	Vdb         *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts      *PodFacts
	ApplyMethod ApplyMethodType
	ScName      string // Subcluster we are going to reconcile.  Blank if all subclusters.
}

func MakeSubscriptionLabelReconciler(vdbrecon *VerticaDBReconciler,
	vdb *vapi.VerticaDB, pfacts *PodFacts, applyMethod ApplyMethodType, scName string) ReconcileActor {
	return &SubscriptionLabelReconciler{
		VRec:        vdbrecon,
		Vdb:         vdb,
		PFacts:      pfacts,
		ApplyMethod: applyMethod,
		ScName:      scName,
	}
}

// Reconcile will add or remove labels from pods based on shard ownership.
// Pods that have at least one shard owned will have a label added so that it
// receives traffic.  For pods that don't own a shard or about to be scaled down
// will have the label removed so that traffic isn't routed to it.
func (s *SubscriptionLabelReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	var savedRes ctrl.Result
	for pn, pf := range s.PFacts.Detail {
		if s.ScName != "" && pf.subcluster != s.ScName {
			continue
		}
		if res, err := s.reconcilePod(ctx, pn, s.PFacts.Detail[pn]); verrors.IsReconcileAborted(res, err) {
			if err == nil {
				// If we fail due to a requeue, we will attempt to reconcile other pods before ultimately bailing out.
				savedRes = res
				continue
			}
			return res, err
		}
	}
	return savedRes, nil
}

// reconcilePod will handle checking for the label of a single pod
func (s *SubscriptionLabelReconciler) reconcilePod(ctx context.Context, pn types.NamespacedName, pf *PodFact) (ctrl.Result, error) {
	var res ctrl.Result
	// We retry if case someone else updated the pod since we last fetched it
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pod := &corev1.Pod{}
		if e := s.VRec.Client.Get(ctx, pn, pod); e != nil {
			// Not found errors are okay to ignore since there is no pod to
			// add/remove a label.
			if errors.IsNotFound(e) {
				return nil
			}
			return e
		}

		// There are 3 cases this reconciler is used:
		// 1) Called after add node
		// 2) Called after pod reschedule + restart
		//    - add because it may have been lost since pod was last rescheduled
		// 3) Called before remove node
		//    - remove if pod is pending delete
		//
		// For 1) and 2), we are going to add labels to qualify pods.  For 2),
		// we will reschedule as this reconciler is usually paired with a
		// rebalance_shards() call.
		//
		// For 3), we are going to remove labels so that client connections
		// stopped getting routed there.
		patch := client.MergeFrom(pod.DeepCopy())
		switch s.ApplyMethod {
		case AddNodeApplyMethod, PodRescheduleApplyMethod:
			if pf.upNode && pf.shardSubscriptions > 0 && !pf.pendingDelete {
				pod.Labels[builder.AcceptClientConnectionsLabel] = builder.AcceptClientConnectionsVal
			}
		case DelNodeApplyMethod:
			if pf.pendingDelete {
				delete(pod.Labels, builder.AcceptClientConnectionsLabel)
			}
		}

		err := s.VRec.Client.Patch(ctx, pod, patch)
		if err != nil {
			return err
		}

		if s.ApplyMethod == AddNodeApplyMethod && pf.upNode && pf.shardSubscriptions == 0 && !pf.pendingDelete {
			s.VRec.Log.Info("Will requeue reconciliation because pod does not have any shard subscriptions yet", "name", pf.name)
			res.Requeue = true
		}
		return nil
	})
	return res, err
}
