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

package metrics

import (
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	k8sMetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	// The namespace for all metrics.  This ends up being a prefix of the name
	// of each of the metrics.
	Namespace = "vertica"

	// The subsystem is the second part of the name.  This comes after the
	// namespace and before the metric name.
	UpgradeSubsystem        = "upgrade"
	ClusterRestartSubsystem = "cluster_restart"
	NodesRestartSubsystem   = "nodes_restart"
	SubclusterSubsystem     = "subclusters"

	// Names of the labels that we can apply to metrics.
	NamespaceLabel        = "namespace"
	VerticaDBLabel        = "verticadb"
	SubclusterOidLabel    = "subcluster_oid"
	ReviveInstanceIDLabel = "revive_instance_id"
)

var (
	AdminToolsBucket = []float64{1, 5, 10, 30, 60, 120, 300, 600}

	UpgradeCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: UpgradeSubsystem,
			Name:      "total",
			Help:      "The number of times the operator performed an upgrade caused by an image change",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel},
	)
	ClusterRestartAttempt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: ClusterRestartSubsystem,
			Name:      "attempted_total",
			Help:      "The number of times we attempted a full cluster restart",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel},
	)
	ClusterRestartFailure = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: ClusterRestartSubsystem,
			Name:      "failed_total",
			Help:      "The number of times we failed when attempting a full cluster restart",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel},
	)
	ClusterRestartDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: Namespace,
			Subsystem: ClusterRestartSubsystem,
			Name:      "seconds",
			Help:      "The number of seconds it took to do a full cluster restart",
			Buckets:   AdminToolsBucket,
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel},
	)
	NodesRestartAttempt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: NodesRestartSubsystem,
			Name:      "attempted_total",
			Help:      "The number of times we attempted to restart down nodes",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel},
	)
	NodesRestartFailed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: NodesRestartSubsystem,
			Name:      "failed_total",
			Help:      "The number of times we failed when trying to restart down nodes",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel},
	)
	NodesRestartDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: Namespace,
			Subsystem: NodesRestartSubsystem,
			Name:      "seconds",
			Help:      "The number of seconds it took to restart down nodes",
			Buckets:   AdminToolsBucket,
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel},
	)
	SubclusterCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: SubclusterSubsystem,
			Name:      "count",
			Help:      "The number of subclusters that exist",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel},
	)
	TotalNodeCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "total_nodes_count",
			Help:      "The number of nodes that currently exist",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel, SubclusterOidLabel},
	)
	RunningNodeCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "running_nodes_count",
			Help:      "The number of nodes that have a running pod associated with it",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel, SubclusterOidLabel},
	)
	UpNodeCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "up_nodes_count",
			Help:      "The number of nodes that have vertica running and can accept connections",
		},
		[]string{NamespaceLabel, VerticaDBLabel, ReviveInstanceIDLabel, SubclusterOidLabel},
	)
	// Add new metrics above this comment.
	//
	// Once a metric is added a few other things need to be updated:
	// 1. Register the metric in then init() function
	// 2. If the metric has subcluster labels, update the function
	//    HandleSubclusterDelete so that we clean them up when a
	//    subcluster is removed.
	// 3. Include the metric in the function HandleVDBDelete.  This is
	//    called when the VerticaDB is deleted, so will do metric cleanup of any
	//    metrics tied to the vdb.
	// 4. Include any metric that only has a verticadb label in HandleVDBInit.
	//    This will ensure all metrics are initialized with value of zero.
)

func init() {
	k8sMetrics.Registry.MustRegister(
		UpgradeCount,
		ClusterRestartAttempt,
		ClusterRestartFailure,
		ClusterRestartDuration,
		NodesRestartAttempt,
		NodesRestartFailed,
		NodesRestartDuration,
		SubclusterCount,
		TotalNodeCount,
		RunningNodeCount,
		UpNodeCount,
	)
}

// HandleSubclusterDelete will cleanup metrics upon subcluster
// deletion.  It will clear out any metrics that are subcluster specific.
func HandleSubclusterDelete(vdb *vapi.VerticaDB, scOid string, log logr.Logger) {
	log.Info("Removing metrics with subcluster label", "subcluster oid", scOid)
	labels := prometheus.Labels{
		NamespaceLabel:     vdb.Namespace,
		VerticaDBLabel:     vdb.Name,
		SubclusterOidLabel: scOid,
	}
	TotalNodeCount.DeletePartialMatch(labels)
	RunningNodeCount.DeletePartialMatch(labels)
	UpNodeCount.DeletePartialMatch(labels)
}

// HandleVDBDelete will cleanup metrics when we find out that the
// VerticaDB no longer exists.  This should include all metrics that include the
// VerticaDB name in its metrics.
func HandleVDBDelete(namespaceName, vdbName string, log logr.Logger) {
	log.Info("Removing metrics with vdb label", "vdb", vdbName)
	// Fill out the labels that we do know. We use DeletePartialMatch to delete
	// metrics that have label values that we don't know (e.g. subcluster name).
	labels := prometheus.Labels{NamespaceLabel: namespaceName, VerticaDBLabel: vdbName}
	UpgradeCount.DeletePartialMatch(labels)
	ClusterRestartAttempt.DeletePartialMatch(labels)
	ClusterRestartFailure.DeletePartialMatch(labels)
	ClusterRestartDuration.DeletePartialMatch(labels)
	NodesRestartAttempt.DeletePartialMatch(labels)
	NodesRestartFailed.DeletePartialMatch(labels)
	NodesRestartDuration.DeletePartialMatch(labels)
	SubclusterCount.DeletePartialMatch(labels)
	TotalNodeCount.DeletePartialMatch(labels)
	RunningNodeCount.DeletePartialMatch(labels)
	UpNodeCount.DeletePartialMatch(labels)
}

// HandleVDBInit will initialized metrics that use verticadb as a
// label.  This is necessary to fill in a missing series with a known verticaDB.
// Otherwise, a metric won't be displayed until we have set some value to it.
// This may break dashboards that assume the metric exists.
func HandleVDBInit(vdb *vapi.VerticaDB) {
	reviveInstanceID := getReviveInstanceID(vdb)
	// Intentionally leaving out the pod/node metrics because we don't know
	// the subcluster names.
	UpgradeCount.WithLabelValues(vdb.Namespace, vdb.Name, reviveInstanceID)
	ClusterRestartAttempt.WithLabelValues(vdb.Namespace, vdb.Name, reviveInstanceID)
	ClusterRestartFailure.WithLabelValues(vdb.Namespace, vdb.Name, reviveInstanceID)
	ClusterRestartDuration.WithLabelValues(vdb.Namespace, vdb.Name, reviveInstanceID)
	NodesRestartAttempt.WithLabelValues(vdb.Namespace, vdb.Name, reviveInstanceID)
	NodesRestartFailed.WithLabelValues(vdb.Namespace, vdb.Name, reviveInstanceID)
	NodesRestartDuration.WithLabelValues(vdb.Namespace, vdb.Name, reviveInstanceID)
}

// MakeVDBLabels return a prometheus.Labels that includes the VerticaDB name
func MakeVDBLabels(vdb *vapi.VerticaDB) prometheus.Labels {
	return prometheus.Labels{
		NamespaceLabel:        vdb.Namespace,
		VerticaDBLabel:        vdb.Name,
		ReviveInstanceIDLabel: getReviveInstanceID(vdb),
	}
}

// MakeSubclusterLabels returns a prometheus.Labels that includes the VerticaDB
// and subcluster name.
func MakeSubclusterLabels(vdb *vapi.VerticaDB, scOid string) prometheus.Labels {
	return prometheus.Labels{
		NamespaceLabel:        vdb.Namespace,
		VerticaDBLabel:        vdb.Name,
		ReviveInstanceIDLabel: getReviveInstanceID(vdb),
		SubclusterOidLabel:    scOid,
	}
}

// getReviveInstanceID returns the revive instance ID stored in the vdb, or an
// empty string if not present yet.
func getReviveInstanceID(vdb *vapi.VerticaDB) string {
	if vdb.Annotations == nil {
		return ""
	}
	reviveInstanceID := vdb.Annotations[vmeta.ReviveInstanceIDAnnotation]
	return reviveInstanceID
}
