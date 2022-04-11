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
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
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
	AddNodeApplyMethod       ApplyMethodType = "Add"           // Called after a db_add_node
	PodRescheduleApplyMethod ApplyMethodType = "PodReschedule" // Called after pod was rescheduled and vertica restarted
	DelNodeApplyMethod       ApplyMethodType = "RemoveNode"    // Called before a db_remove_node
)

type ClientRoutingLabelReconciler struct {
	VRec        *VerticaDBReconciler
	Vdb         *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts      *PodFacts
	ApplyMethod ApplyMethodType
	ScName      string // Subcluster we are going to reconcile.  Blank if all subclusters.
}

func MakeClientRoutingLabelReconciler(vdbrecon *VerticaDBReconciler,
	vdb *vapi.VerticaDB, pfacts *PodFacts, applyMethod ApplyMethodType, scName string) controllers.ReconcileActor {
	return &ClientRoutingLabelReconciler{
		VRec:        vdbrecon,
		Vdb:         vdb,
		PFacts:      pfacts,
		ApplyMethod: applyMethod,
		ScName:      scName,
	}
}

// Reconcile will add or remove labels that control whether it accepts client
// connections.  Pods that have at least one shard owned will have a label added
// so that it receives traffic.  For pods that don't own a shard or about to be
// scaled down will have the label removed so that traffic isn't routed to it.
func (c *ClientRoutingLabelReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	c.VRec.Log.Info("Reconcile client routing label", "applyMethod", c.ApplyMethod)

	if err := c.PFacts.Collect(ctx, c.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	var savedRes ctrl.Result
	for pn, pf := range c.PFacts.Detail {
		if c.ScName != "" && pf.subcluster != c.ScName {
			continue
		}
		if res, err := c.reconcilePod(ctx, pn, c.PFacts.Detail[pn]); verrors.IsReconcileAborted(res, err) {
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
func (c *ClientRoutingLabelReconciler) reconcilePod(ctx context.Context, pn types.NamespacedName, pf *PodFact) (ctrl.Result, error) {
	var res ctrl.Result
	// We retry if case someone else updated the pod since we last fetched it
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pod := &corev1.Pod{}
		if e := c.VRec.Client.Get(ctx, pn, pod); e != nil {
			// Not found errors are okay to ignore since there is no pod to
			// add/remove a label.
			if errors.IsNotFound(e) {
				return nil
			}
			return e
		}

		patch := client.MergeFrom(pod.DeepCopy())
		c.manipulateRoutingLabelInPod(pod, pf)
		err := c.VRec.Client.Patch(ctx, pod, patch)
		if err != nil {
			return err
		}

		if c.ApplyMethod == AddNodeApplyMethod && pf.upNode && pf.shardSubscriptions == 0 && !pf.pendingDelete {
			c.VRec.Log.Info("Will requeue reconciliation because pod does not have any shard subscriptions yet", "name", pf.name)
			res.Requeue = true
		}
		return nil
	})
	return res, err
}

func (c *ClientRoutingLabelReconciler) manipulateRoutingLabelInPod(pod *corev1.Pod, pf *PodFact) {
	_, labelExists := pod.Labels[builder.ClientRoutingLabel]

	// There are 4 cases this reconciler is used:
	// 1) Called after add node
	// 2) Called after pod reschedule + restart
	// 3) Called before remove node
	// 4) Called before removal of a subcluster
	//
	// For 1) and 2), we are going to add labels to qualify pods.  For 2),
	// we will reschedule as this reconciler is usually paired with a
	// rebalance_shards() call.
	//
	// For 3), we are going to remove labels so that client connections
	// stopped getting routed there.  This only applies to pods that are
	// pending delete.
	//
	// For 4), like 3) we are going to remove labels.  This applies to the
	// entire subcluster, so pending delete isn't checked.
	switch c.ApplyMethod {
	case AddNodeApplyMethod, PodRescheduleApplyMethod:
		if !labelExists && pf.upNode && pf.shardSubscriptions > 0 && !pf.pendingDelete {
			pod.Labels[builder.ClientRoutingLabel] = builder.ClientRoutingVal
			c.VRec.Log.Info("Adding client routing label", "pod",
				pod.Name, "label", fmt.Sprintf("%s=%s", builder.ClientRoutingLabel, builder.ClientRoutingVal))
		}
	case DelNodeApplyMethod:
		if labelExists && pf.pendingDelete {
			delete(pod.Labels, builder.ClientRoutingLabel)
			c.VRec.Log.Info("Removing client routing label", "pod",
				pod.Name, "label", fmt.Sprintf("%s=%s", builder.ClientRoutingLabel, builder.ClientRoutingVal))
		}
	}
}
