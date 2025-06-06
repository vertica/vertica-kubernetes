/*
Copyright [2021-2024] Open Text.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ReasonSucceeded = "Succeeded"
)

// Affinity is used instead of corev1.Affinity and behaves the same.
// This structure is used in some CRs fields to define the "Affinity".
// corev1.Affinity is composed of 3 fields and for each of them,
// there is a x-descriptor. However there is not a x-descriptor for corev1.Affinity itself.
// In this structure, we have the same fields as corev1' but we also added
// the corresponding x-descriptor to each field. That will be useful for the Openshift web console.
type Affinity struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:nodeAffinity"
	// Describes node affinity scheduling rules for the pod.
	// +optional
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty" protobuf:"bytes,1,opt,name=nodeAffinity"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podAffinity"
	// Describes pod affinity scheduling rules (e.g. co-locate this pod in the same node, zone, etc. as some other pod(s)).
	// +optional
	PodAffinity *corev1.PodAffinity `json:"podAffinity,omitempty" protobuf:"bytes,2,opt,name=podAffinity"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podAntiAffinity"
	// Describes pod anti-affinity scheduling rules (e.g. avoid putting this pod in the same node, zone, etc. as some other pod(s)).
	// +optional
	PodAntiAffinity *corev1.PodAntiAffinity `json:"podAntiAffinity,omitempty" protobuf:"bytes,3,opt,name=podAntiAffinity"`
}

// IsStatusConditionTrue returns true when the conditionType is present and set to
// `metav1.ConditionTrue`
func (vscr *VerticaScrutinize) IsStatusConditionTrue(statusCondition string) bool {
	return meta.IsStatusConditionTrue(vscr.Status.Conditions, statusCondition)
}

// IsStatusConditionFalse returns true when the conditionType is present and set to
// `metav1.ConditionFalse`
func (vscr *VerticaScrutinize) IsStatusConditionFalse(statusCondition string) bool {
	return meta.IsStatusConditionFalse(vscr.Status.Conditions, statusCondition)
}

func (vscr *VerticaScrutinize) ExtractNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      vscr.ObjectMeta.Name,
		Namespace: vscr.ObjectMeta.Namespace,
	}
}

func MakeSampleVscrName() types.NamespacedName {
	return types.NamespacedName{Name: "vscr-sample", Namespace: "default"}
}

// MakeVscr will make an VerticaScrutinize for test purposes
func MakeVscr() *VerticaScrutinize {
	VDBNm := MakeVDBName()
	nm := MakeSampleVscrName()
	return &VerticaScrutinize{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupVersion.String(),
			Kind:       VerticaScrutinizeKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			UID:         "abcdef-123-ttt",
			Annotations: make(map[string]string),
		},
		Spec: VerticaScrutinizeSpec{
			VerticaDBName: VDBNm.Name,
			Labels:        make(map[string]string),
			Annotations:   make(map[string]string),
		},
	}
}

// GenerateLogAgeTime returns a string in the format of YYYY-MM-DD HH [+/-XX]
func GenerateLogAgeTime(hourOffset time.Duration, timeZone string) string {
	timeOffset := time.Now().Add(hourOffset * time.Hour)
	timeOffsetFormatted := fmt.Sprintf("%s %s", timeOffset.Format("2006-01-02"), strconv.Itoa(timeOffset.Hour()))

	if timeZone != "" {
		timeOffsetFormatted = fmt.Sprintf("%s %s", timeOffsetFormatted, timeZone)
	}
	return timeOffsetFormatted
}

// FindStatusCondition finds the conditionType in conditions.
func (vscr *VerticaScrutinize) FindStatusCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(vscr.Status.Conditions, conditionType)
}

// IsStatusConditionPresent returns true when conditionType is present
func (vscr *VerticaScrutinize) IsStatusConditionPresent(conditionType string) bool {
	cond := vscr.FindStatusCondition(conditionType)
	return cond != nil
}

func (vrpq *VerticaRestorePointsQuery) ExtractNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      vrpq.ObjectMeta.Name,
		Namespace: vrpq.ObjectMeta.Namespace,
	}
}

func (vrpq *VerticaRestorePointsQuery) IsStatusConditionTrue(statusCondition string) bool {
	return meta.IsStatusConditionTrue(vrpq.Status.Conditions, statusCondition)
}

func (vrpq *VerticaRestorePointsQuery) IsStatusConditionFalse(statusCondition string) bool {
	return meta.IsStatusConditionFalse(vrpq.Status.Conditions, statusCondition)
}

func (vrpq *VerticaRestorePointsQuery) IsStatusConditionPresent(statusCondition string) bool {
	return meta.FindStatusCondition(vrpq.Status.Conditions, statusCondition) != nil
}

// FindStatusCondition finds the conditionType in conditions.
func (vrep *VerticaReplicator) FindStatusCondition(conditionType string) *metav1.Condition {
	return meta.FindStatusCondition(vrep.Status.Conditions, conditionType)
}

func (vrep *VerticaReplicator) IsStatusConditionTrue(statusCondition string) bool {
	return meta.IsStatusConditionTrue(vrep.Status.Conditions, statusCondition)
}

func (vrep *VerticaReplicator) IsStatusConditionFalse(statusCondition string) bool {
	return meta.IsStatusConditionFalse(vrep.Status.Conditions, statusCondition)
}

func (vrep *VerticaReplicator) IsStatusConditionPresent(statusCondition string) bool {
	return meta.FindStatusCondition(vrep.Status.Conditions, statusCondition) != nil
}

// GetHPAMetrics extract an return hpa metrics from MetricDefinition struct.
func (v *VerticaAutoscaler) GetHPAMetrics() []autoscalingv2.MetricSpec {
	metrics := make([]autoscalingv2.MetricSpec, len(v.Spec.CustomAutoscaler.Hpa.Metrics))
	for i := range v.Spec.CustomAutoscaler.Hpa.Metrics {
		metrics[i] = v.Spec.CustomAutoscaler.Hpa.Metrics[i].Metric
	}
	return metrics
}

// ValidatePrometheusAuthBasic will check if required key exists for type PrometheusAuthBasic
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthBasic(secretData map[string][]byte) error {
	if _, ok := secretData[PrometheusSecretKeyUsername]; !ok {
		return errors.New("username not found in secret")
	}
	if _, ok := secretData[PrometheusSecretKeyPassword]; !ok {
		return errors.New("password not found in secret")
	}
	return nil
}

// ValidatePrometheusAuthBearer will check if required key exists for type PrometheusAuthBearer
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthBearer(secretData map[string][]byte) error {
	if _, ok := secretData[PrometheusSecretKeyBearerToken]; !ok {
		return errors.New("bearerToken not found in secret")
	}
	return nil
}

// ValidatePrometheusAuthTLS will check if required key exists for type PrometheusAuthTLS
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthTLS(secretData map[string][]byte) error {
	if _, ok := secretData[PrometheusSecretKeyCa]; !ok {
		return errors.New("ca not found in secret")
	}
	if _, ok := secretData[PrometheusSecretKeyCert]; !ok {
		return errors.New("cert not found in secret")
	}
	if _, ok := secretData[PrometheusSecretKeyKey]; !ok {
		return errors.New("key not found in secret")
	}
	return nil
}

// ValidatePrometheusAuthCustom will check if required key exists for type PrometheusAuthCustom
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthCustom(secretData map[string][]byte) error {
	if _, ok := secretData[PrometheusSecretKeyCustomAuthHeader]; !ok {
		return errors.New("customAuthHeader not found in secret")
	}
	if _, ok := secretData[PrometheusSecretKeyCustomAuthValue]; !ok {
		return errors.New("customAuthValue not found in secret")
	}
	return nil
}

// ValidatePrometheusAuthTLSAndBasic will check if required key exists for type PrometheusAuthTLSAndBasic
func (authmode *PrometheusAuthModes) ValidatePrometheusAuthTLSAndBasic(secretData map[string][]byte) error {
	if err := authmode.ValidatePrometheusAuthBasic(secretData); err != nil {
		return err
	}
	if err := authmode.ValidatePrometheusAuthTLS(secretData); err != nil {
		return err
	}
	return nil
}

// GetMap Convert PrometheusSpec to map[string]string
func (p *PrometheusSpec) GetMap() map[string]string {
	result := make(map[string]string)
	result["serverAddress"] = p.ServerAddress
	result["query"] = p.Query
	result["threshold"] = fmt.Sprintf("%d", p.Threshold)
	// Only add ScaleInThreshold if it is non-zero
	if p.ScaleInThreshold != 0 {
		result["activationThreshold"] = fmt.Sprintf("%d", p.ScaleInThreshold)
	}

	return result
}

// GetMap converts CPUMemorySpec to map[string]string
func (r *CPUMemorySpec) GetMap() map[string]string {
	result := make(map[string]string)
	result["value"] = fmt.Sprintf("%d", r.Threshold)
	return result
}

// GetMetadata returns the metric parameters map
func (s *ScaleTrigger) GetMetadata() map[string]string {
	if s.IsPrometheusMetric() {
		return s.Prometheus.GetMap()
	}
	return s.Resource.GetMap()
}

func (s *ScaleTrigger) IsNil() bool {
	return s.Prometheus == nil && s.Resource == nil
}

func (s *ScaleTrigger) IsPrometheusMetric() bool {
	return s.Type == PrometheusTriggerType || s.Type == ""
}

func (s *ScaleTrigger) GetUnsafeSslStr() string {
	return strconv.FormatBool(s.Prometheus.UnsafeSsl)
}

func (s *ScaleTrigger) GetType() string {
	if s.Type == "" {
		return string(PrometheusTriggerType)
	}
	return string(s.Type)
}

// GetIsSandboxPrimary checks the subcluster isSandboxPrimary by its name
func (v *VerticaDBSpec) GetIsSandboxPrimary(scName string) bool {
	for i := range v.Subclusters {
		if v.Subclusters[i].Name == scName {
			return v.Subclusters[i].IsSandboxPrimary
		}
	}

	return false
}

// MakeScaledObjectSpec builds a sample scaleObjectSpec.
// This is intended for test purposes.
func MakeScaledObjectSpec() *ScaledObjectSpec {
	return &ScaledObjectSpec{
		MinReplicas:     &[]int32{3}[0],
		MaxReplicas:     &[]int32{6}[0],
		PollingInterval: &[]int32{5}[0],
		Metrics: []ScaleTrigger{
			{
				Name: "sample-metric",
				Prometheus: &PrometheusSpec{
					ServerAddress: "http://localhost",
					Query:         "query",
					Threshold:     5,
				},
			},
		},
	}
}

// HasScaleInThreshold returns true if scale in threshold is set
func (v *VerticaAutoscaler) HasScaleInThreshold() bool {
	if !v.IsHpaEnabled() {
		return false
	}
	for i := range v.Spec.CustomAutoscaler.Hpa.Metrics {
		m := &v.Spec.CustomAutoscaler.Hpa.Metrics[i]
		if m.ScaleInThreshold != nil {
			return true
		}
	}
	return false
}

// GetMinReplicas calculates the minReplicas based on the scale in
// threshold, and returns it
func (v *VerticaAutoscaler) GetMinReplicas() *int32 {
	vasCopy := v.DeepCopy()
	if v.HasScaleInThreshold() {
		return &vasCopy.Spec.TargetSize
	}
	return vasCopy.Spec.CustomAutoscaler.Hpa.MinReplicas
}

// GetMetricMap returns a map whose key is the metric name and the value is
// the metric's definition.
func (v *VerticaAutoscaler) GetMetricMap() map[string]*MetricDefinition {
	mMap := make(map[string]*MetricDefinition)
	for i := range v.Spec.CustomAutoscaler.Hpa.Metrics {
		m := &v.Spec.CustomAutoscaler.Hpa.Metrics[i]
		var name string
		if m.Metric.Pods != nil {
			name = m.Metric.Pods.Metric.Name
		} else if m.Metric.Object != nil {
			name = m.Metric.Object.Metric.Name
		} else if m.Metric.External != nil {
			name = m.Metric.External.Metric.Name
		} else if m.Metric.Resource != nil {
			name = m.Metric.Resource.Name.String()
		} else {
			name = m.Metric.ContainerResource.Name.String()
		}
		mMap[name] = m
	}
	return mMap
}

func MakeSampleVrpqName() types.NamespacedName {
	return types.NamespacedName{Name: "vrpq-sample", Namespace: "default"}
}

// MakeVrpq will make an VerticaRestorePointsQuery for test purposes
func MakeVrpq() *VerticaRestorePointsQuery {
	VDBNm := MakeVDBName()
	nm := MakeSampleVrpqName()
	vrpq := &VerticaRestorePointsQuery{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupVersion.String(),
			Kind:       RestorePointsQueryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
			UID:       "zxcvbn-ghi-lkm",
		},
		Spec: VerticaRestorePointsQuerySpec{
			VerticaDBName: VDBNm.Name,
			FilterOptions: &VerticaRestorePointQueryFilterOptions{
				ArchiveName: archiveNm,
			},
		},
	}
	return vrpq
}

func MakeSampleVrepName() types.NamespacedName {
	return types.NamespacedName{Name: "vrep-sample", Namespace: "default"}
}

// MakeVrep will make a VerticaReplicator for test purposes
func MakeVrep() *VerticaReplicator {
	sourceVDBNm := MakeSourceVDBName()
	targetVDBNm := MakeTargetVDBName()
	nm := MakeSampleVrepName()
	vrep := &VerticaReplicator{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupVersion.String(),
			Kind:       VerticaReplicatorKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
			UID:       "zxcvbn-ghi-lkm-xyz",
		},
		Spec: VerticaReplicatorSpec{
			Source: VerticaReplicatorSourceDatabaseInfo{
				VerticaReplicatorDatabaseInfo: VerticaReplicatorDatabaseInfo{
					VerticaDB: sourceVDBNm.Name,
				},
			},
			Target: VerticaReplicatorTargetDatabaseInfo{
				VerticaReplicatorDatabaseInfo: VerticaReplicatorDatabaseInfo{
					VerticaDB: targetVDBNm.Name,
				},
			},
		},
	}
	return vrep
}

func GenCompatibleFQDNHelper(scName string) string {
	m := regexp.MustCompile(`_`)
	return m.ReplaceAllString(scName, "-")
}

// ptrOrNil is a helper function to create a new pointer if not nil
func ptrOrNil[T any](val *T) *T {
	if val == nil {
		return nil
	}
	newVal := *val
	return &newVal
}
