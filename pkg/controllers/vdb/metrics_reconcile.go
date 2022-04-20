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

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/metrics"
	ctrl "sigs.k8s.io/controller-runtime"
)

// MetricReconciler will refresh any metrics based on latest podfacts
type MetricReconciler struct {
	VRec   *VerticaDBReconciler
	Vdb    *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts *PodFacts
}

// subclusterGaugeDetail will collect information for each gauge.  This is
// intended to be use per subcluster.
type subclusterGaugeDetail struct {
	podCount     float64
	runningCount float64
	readyCount   float64
}

// MakeMetricReconciler will build a MetricReconciler object
func MakeMetricReconciler(vrec *VerticaDBReconciler, vdb *vapi.VerticaDB, pfacts *PodFacts) controllers.ReconcileActor {
	return &MetricReconciler{VRec: vrec, Vdb: vdb, PFacts: pfacts}
}

// Reconcile will update the metrics based on the pod facts
func (p *MetricReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := p.PFacts.Collect(ctx, p.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Initialized any metrics that use the vdb as label.  This sets all of the
	// metrics for the current vdb to zero.
	metrics.HandleVDBInit(p.Vdb)

	// Update any gauges that we track for subcluster events
	rawMetrics := p.captureRawMetrics()
	metrics.SubclusterCount.With(metrics.MakeVDBLabels(p.Vdb)).Set(float64(len(rawMetrics)))
	for scName, detail := range rawMetrics {
		scLabels := metrics.MakeSubclusterLabels(p.Vdb, scName)
		metrics.SubclusterPodCount.With(scLabels).Set(detail.podCount)
		metrics.SubclusterReadyPodCount.With(scLabels).Set(detail.readyCount)
		metrics.SubclusterRunningPodCount.With(scLabels).Set(detail.runningCount)
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
		if _, ok := scMap[pf.subcluster]; !ok {
			continue
		}
		if _, ok := scGaugeSummary[pf.subcluster]; !ok {
			scGaugeSummary[pf.subcluster] = &subclusterGaugeDetail{}
		}
		scGaugeSummary[pf.subcluster].podCount++
		if pf.isPodRunning {
			scGaugeSummary[pf.subcluster].runningCount++
		}
		if pf.upNode {
			scGaugeSummary[pf.subcluster].readyCount++
		}
	}
	return scGaugeSummary
}
