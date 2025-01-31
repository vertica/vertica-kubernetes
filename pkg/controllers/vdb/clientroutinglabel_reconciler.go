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
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ApplyMethodType string

const (
	// Called after a db_add_node
	AddNodeApplyMethod ApplyMethodType = "Add"
	// Called after pod was rescheduled and vertica restarted
	PodRescheduleApplyMethod ApplyMethodType = "PodReschedule"
	// Called before a db_remove_node
	DelNodeApplyMethod ApplyMethodType = "RemoveNode"
	// Called as part of a drain operation. We want no traffic at the node.
	DrainNodeApplyMethod ApplyMethodType = "DrainNode"
	// Called when redirect connections during online upgrade. We want no traffic at the proxy of old cluster.
	DisableProxyApplyMethod ApplyMethodType = "DisableProxy"
)

type ClientRoutingLabelReconciler struct {
	Rec            config.ReconcilerInterface
	Vdb            *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log            logr.Logger
	PFacts         *podfacts.PodFacts
	ApplyMethod    ApplyMethodType
	ScName         string // Subcluster we are going to reconcile.  Blank if all subclusters.
	DisableRouting bool
}

func MakeClientRoutingLabelReconciler(recon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts, applyMethod ApplyMethodType, scName string) controllers.ReconcileActor {
	return &ClientRoutingLabelReconciler{
		Rec:         recon,
		Vdb:         vdb,
		Log:         log.WithName("ClientRoutingLabelReconciler"),
		PFacts:      pfacts,
		ApplyMethod: applyMethod,
		ScName:      scName,
	}
}

func MakeClientRoutingLabelReconcilerWithDisableRouting(recon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts, applyMethod ApplyMethodType, scName string,
	disableRouting bool) controllers.ReconcileActor {
	act := MakeClientRoutingLabelReconciler(recon, log, vdb, pfacts, applyMethod, scName)
	c := act.(*ClientRoutingLabelReconciler)
	c.DisableRouting = disableRouting
	return c
}

// Reconcile will add or remove labels that control whether it accepts client
// connections.  Pods that have at least one shard owned will have a label added
// so that it receives traffic.  For pods that don't own a shard or about to be
// scaled down will have the label removed so that traffic isn't routed to it.
func (c *ClientRoutingLabelReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	c.Log.Info("Reconcile client routing label", "applyMethod", c.ApplyMethod)

	// If we are using vproxy, we don't need to modify client routing label on pods
	if vmeta.UseVProxy(c.Vdb.Annotations) {
		if c.ApplyMethod == DelNodeApplyMethod || c.ApplyMethod == DrainNodeApplyMethod {
			c.Log.Info("Skipping client routing label reconcile for proxy pods when removing node or draining node", "applyMethod", c.ApplyMethod)
			return ctrl.Result{}, nil
		}
		return c.reconcileProxy(ctx)
	}

	if err := c.PFacts.Collect(ctx, c.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	var savedRes ctrl.Result
	for pn, pf := range c.PFacts.Detail {
		if c.ScName != "" && pf.GetSubclusterName() != c.ScName {
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

// reconcoileProxy will reconcile client routing label for all proxy pods
func (c *ClientRoutingLabelReconciler) reconcileProxy(ctx context.Context) (ctrl.Result, error) {
	scs := []string{}
	if c.ScName != "" {
		scs = append(scs, c.ScName)
	} else {
		scs = c.Vdb.GetSubclustersInSandbox(c.PFacts.SandboxName)
	}
	for _, sc := range scs {
		if res, err := c.reconcileProxyForSC(ctx, sc); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileProxyForSC will reconcile client routing label for the proxy pods in target subcluster
func (c *ClientRoutingLabelReconciler) reconcileProxyForSC(ctx context.Context, scName string) (ctrl.Result, error) {
	scMap := c.Vdb.GenSubclusterMap()
	sc, ok := scMap[scName]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("subcluster %q not found when reconciling client routing label for proxy", scName)
	}
	pods := corev1.PodList{}
	proxyLabels := map[string]string{
		vmeta.ProxyPodSelectorLabel:   vmeta.ProxyPodSelectorVal,
		vmeta.VDBInstanceLabel:        c.Vdb.Name,
		vmeta.DeploymentSelectorLabel: sc.GetVProxyDeploymentName(c.Vdb),
	}
	listOps := &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set(proxyLabels)),
		Namespace:     c.Vdb.GetNamespace(),
	}
	err := c.Rec.GetClient().List(ctx, &pods, listOps)
	if err != nil {
		c.Log.Error(err, "unable to list proxy pods")
		return ctrl.Result{}, err
	}
	for inx := range pods.Items {
		pod := &pods.Items[inx]
		patch := client.MergeFrom(pod.DeepCopy())
		labelVal, labelExists := pod.Labels[vmeta.ClientRoutingLabel]
		if c.ApplyMethod == DisableProxyApplyMethod {
			if !labelExists || labelVal != vmeta.ClientRoutingVal {
				continue
			}
			delete(pod.Labels, vmeta.ClientRoutingLabel)
			c.Log.Info("Removing client routing label from proxy pod", "pod",
				pod.Name, "label", fmt.Sprintf("%s=%s", vmeta.ClientRoutingLabel, vmeta.ClientRoutingVal))
		} else {
			if labelExists && labelVal == vmeta.ClientRoutingVal {
				continue
			}
			// Check if the pod's conditions include 'Ready' being true
			podIsReady := false
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					podIsReady = true
					break
				}
			}
			if pod.Status.Phase != corev1.PodRunning || !podIsReady {
				c.Log.Info("Requeue because proxy pod is not ready", "pod", pod.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			pod.Labels[vmeta.ClientRoutingLabel] = vmeta.ClientRoutingVal
			c.Log.Info("Adding client routing label to proxy pod", "pod",
				pod.Name, "label", fmt.Sprintf("%s=%s", vmeta.ClientRoutingLabel, vmeta.ClientRoutingVal))
		}
		err := c.Rec.GetClient().Patch(ctx, pod, patch)
		if err != nil {
			return ctrl.Result{}, err
		}
		c.Log.Info("Proxy pod has been patched", "pod", pod.Name, "labels", pod.Labels)
	}
	return ctrl.Result{}, nil
}

// reconcilePod will handle checking for the label of a single pod
func (c *ClientRoutingLabelReconciler) reconcilePod(ctx context.Context, pn types.NamespacedName,
	pf *podfacts.PodFact) (ctrl.Result, error) {
	var res ctrl.Result
	// We retry if case someone else updated the pod since we last fetched it
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		pod := &corev1.Pod{}
		if e := c.Rec.GetClient().Get(ctx, pn, pod); e != nil {
			// Not found errors are okay to ignore since there is no pod to
			// add/remove a label.
			if errors.IsNotFound(e) {
				return nil
			}
			return e
		}

		patch := client.MergeFrom(pod.DeepCopy())
		c.manipulateRoutingLabelInPod(pod, pf)
		err := c.Rec.GetClient().Patch(ctx, pod, patch)
		if err != nil {
			return err
		}
		c.Log.Info("pod has been patched", "name", pod.Name, "labels", pod.Labels)

		if c.ApplyMethod == AddNodeApplyMethod && c.Vdb.IsEON() && pf.GetUpNode() && pf.GetShardSubscriptions() == 0 && !pf.GetIsPendingDelete() {
			c.Log.Info("Will requeue reconciliation because pod does not have any shard subscriptions yet", "name", pf.GetName())
			res.Requeue = true
		}
		return nil
	})
	return res, err
}

func (c *ClientRoutingLabelReconciler) manipulateRoutingLabelInPod(pod *corev1.Pod, pf *podfacts.PodFact) {
	_, labelExists := pod.Labels[vmeta.ClientRoutingLabel]

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
		if !c.DisableRouting && !labelExists && pf.GetUpNode() && (pf.GetShardSubscriptions() > 0 || !c.Vdb.IsEON()) && !pf.GetIsPendingDelete() {
			pod.Labels[vmeta.ClientRoutingLabel] = vmeta.ClientRoutingVal
			c.Log.Info("Adding client routing label", "pod",
				pod.Name, "label", fmt.Sprintf("%s=%s", vmeta.ClientRoutingLabel, vmeta.ClientRoutingVal))
		}
	case DelNodeApplyMethod, DrainNodeApplyMethod:
		if labelExists && (c.ApplyMethod == DrainNodeApplyMethod || pf.GetIsPendingDelete()) {
			delete(pod.Labels, vmeta.ClientRoutingLabel)
			c.Log.Info("Removing client routing label", "pod",
				pod.Name, "label", fmt.Sprintf("%s=%s", vmeta.ClientRoutingLabel, vmeta.ClientRoutingVal))
		}
	case DisableProxyApplyMethod:
		c.Log.Info("Skipping updating client routing label for pod with a wrong apply method", "pod", pod.Name)
	}
}
