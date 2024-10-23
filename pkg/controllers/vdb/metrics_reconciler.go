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
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/metrics"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// MetricReconciler will refresh any metrics based on latest podfacts
type MetricReconciler struct {
	Log     logr.Logger
	VRec    *VerticaDBReconciler
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts  *podfacts.PodFacts
	PRunner cmds.PodRunner
}

// subclusterGaugeDetail will collect information for each gauge.  This is
// intended to be use per subcluster.
type subclusterGaugeDetail struct {
	podCount     float64
	runningCount float64
	readyCount   float64
}

// MakeMetricReconciler will build a MetricReconciler object
func MakeMetricReconciler(vrec *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	prunner cmds.PodRunner, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &MetricReconciler{
		Log:     log.WithName("MetricReconciler"),
		VRec:    vrec,
		Vdb:     vdb,
		PFacts:  pfacts,
		PRunner: prunner,
	}
}

// Reconcile will update the metrics based on the pod facts
func (p *MetricReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if err := p.PFacts.Collect(ctx, p.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile of the metrics depends on knowing the revive_instance_id as
	// this is used as one of the labels. Capture that if its missing. We will
	// not continue if we can't get it.
	if !p.Vdb.HasReviveInstanceIDAnnotation() {
		if err := p.setReviveInstanceIDAnnotation(ctx); err != nil {
			return ctrl.Result{}, err
		}
		if !p.Vdb.HasReviveInstanceIDAnnotation() {
			p.Log.Info("Skipping metrics reconcile until we know the revive_instance_id")
			return ctrl.Result{}, nil
		}
	}

	// Initialized any metrics that use the vdb as label.  This sets all of the
	// metrics for the current vdb to zero.
	metrics.HandleVDBInit(p.Vdb)

	// Update any gauges that we track for subcluster events
	rawMetrics := p.captureRawMetrics()
	metrics.SubclusterCount.With(metrics.MakeVDBLabels(p.Vdb)).Set(float64(len(rawMetrics)))
	for scOid, detail := range rawMetrics {
		scLabels := metrics.MakeSubclusterLabels(p.Vdb, scOid)
		metrics.TotalNodeCount.With(scLabels).Set(detail.podCount)
		metrics.UpNodeCount.With(scLabels).Set(detail.readyCount)
		metrics.RunningNodeCount.With(scLabels).Set(detail.runningCount)
	}

	return ctrl.Result{}, nil
}

// captureRawMetrics will summarize the raw metrics for the subcluster gauges
func (p *MetricReconciler) captureRawMetrics() map[string]*subclusterGaugeDetail {
	scGaugeSummary := map[string]*subclusterGaugeDetail{}
	scMap := p.Vdb.GenSubclusterMap()
	for _, pf := range p.PFacts.Detail {
		// Only use subclusters from the Vdb.  We omit ones that are scheduled for
		// deletion because we need to clear metrics for those deleted subclusters
		// before we actually remove their statefulsets.
		if _, ok := scMap[pf.GetSubclusterName()]; !ok {
			continue
		}
		if _, ok := scGaugeSummary[pf.GetSubclusterOid()]; !ok {
			scGaugeSummary[pf.GetSubclusterOid()] = &subclusterGaugeDetail{}
		}
		scGaugeSummary[pf.GetSubclusterOid()].podCount++
		if pf.GetIsPodRunning() {
			scGaugeSummary[pf.GetSubclusterOid()].runningCount++
		}
		if pf.GetUpNode() {
			scGaugeSummary[pf.GetSubclusterOid()].readyCount++
		}
	}
	return scGaugeSummary
}

// setReviveInstanceIDAnnotation will attempt to set the revive_instance_id
// annotation in the vdb. This may fail for valid cases (e.g. vertica isn't up),
// so its up to the caller to check that the annotaiton was set and act
// accordingly.
func (p *MetricReconciler) setReviveInstanceIDAnnotation(ctx context.Context) error {
	pf, ok := p.PFacts.FindFirstUpPod(true, "")
	if !ok {
		return nil
	}

	cmd := []string{"-tAc", "select revive_instance_id from vs_databases"}
	op, _, err := p.PRunner.ExecVSQL(ctx, pf.GetName(), names.ServerContainer, cmd...)
	if err != nil {
		return err
	}
	ann := map[string]string{vmeta.ReviveInstanceIDAnnotation: strings.TrimSpace(op)}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch to get latest Vdb incase this is a retry
		err := p.VRec.Client.Get(ctx, p.Vdb.ExtractNamespacedName(), p.Vdb)
		if err != nil {
			return err
		}
		if p.Vdb.MergeAnnotations(ann) {
			return p.VRec.Client.Update(ctx, p.Vdb)
		}
		return nil
	})
}
