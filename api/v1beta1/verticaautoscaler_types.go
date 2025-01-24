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

//nolint:lll
package v1beta1

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// VerticaAutoscalerSpec defines the desired state of VerticaAutoscaler
type VerticaAutoscalerSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The name of the VerticaDB CR that this autoscaler is defined for.  The
	// VerticaDB object must exist in the same namespace as this object.
	VerticaDBName string `json:"verticaDBName"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:default:="Subcluster"
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Pod","urn:alm:descriptor:com.tectonic.ui:select:Subcluster"}
	// This defines how the scaling will happen.  This can be one of the following:
	// - Subcluster: Scaling will be achieved by creating or deleting entire subclusters.
	//   The template for new subclusters are either the template if filled out
	//   or an existing subcluster that matches the service name.
	// - Pod: Only increase or decrease the size of an existing subcluster.
	//   If multiple subclusters are selected by the serviceName, this will grow
	//   the last subcluster only.
	ScalingGranularity ScalingGranularityType `json:"scalingGranularity"`

	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// This acts as a selector for the subclusters that are being scaled together.
	// Each subcluster has a service name field, which if omitted is the same
	// name as the subcluster name.  Multiple subclusters that have the same
	// service name use the same service object.
	ServiceName string `json:"serviceName"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// When the scaling granularity is Subcluster, this field defines a template
	// to use for when a new subcluster needs to be created.  If size is 0, then
	// the operator will use an existing subcluster to use as the template.  If
	// size is > 0, the service name must match the serviceName parameter.  The
	// name of the new subcluster is always auto generated.  If the name is set
	// here it will be used as a prefix for the new subcluster.  Otherwise, we
	// use the name of this VerticaAutoscaler object as a prefix for all
	// subclusters.
	Template Subcluster `json:"template"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount"
	// This is the total pod count for all subclusters that match the
	// serviceName.  Changing this value may trigger a change in the
	// VerticaDB that is associated with this object.  This value is generally
	// left as zero.  It will get initialized in the operator and then modified
	// via the /scale subresource.
	TargetSize int32 `json:"targetSize"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// This struct allows customization of autoscaling. Custom metrics can be used instead of the memory and cpu metrics.
	// The scaling behavior can also be customized to meet different performance requirements. The maximum and mininum of
	// sizes of the replica sets can be specified to limit the use of resources.
	CustomAutoscaler *CustomAutoscalerSpec `json:"customAutoscaler,omitempty"`
}

// CustomAutoscalerSpec customizes VerticaAutoscaler
type CustomAutoscalerSpec struct {
	Type         string            `json:"type"`
	Hpa          *HPASpec          `json:"hpa,omitempty"`
	ScaledObject *ScaledObjectSpec `json:"scaledObject,omitempty"`
}

type HPASpec struct {
	// +kubebuilder:Minimum:=0
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The miminum number of pods when scaling.
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// +kubebuilder:Minimum:=0
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The maximum number of pods when scaling.
	MaxReplicas int32 `json:"maxReplicas,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The custom metric definition to be used for autocaling.
	Metrics []MetricDefinition `json:"metrics,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Specifies the scaling behavior for both scale up and down.
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behavior,omitempty"`
}

type ScaledObjectSpec struct {
	// +kubebuilder:Minimum:=0
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The miminum number of pods when scaling.
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// +kubebuilder:Minimum:=0
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The maximum number of pods when scaling.
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The time interval at which the scaler will check the metric condition and scale the target (in seconds).
	// If not specified, the default is 30 seconds.
	PollingInterval *int32 `json:"pollingInterval,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Defines the time to wait between scaling actions. This is helpful to avoid constant scaling up/down. Default: 30s.
	CooldownPeriod *int32 `json:"cooldownPeriod,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The list of prometheus queries that will be used for scaling.
	Metrics []ScaleTrigger `json:"metrics"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Specifies the scaling behavior for both scale up and down.
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behavior,omitempty"`
}

type ScaleTrigger struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The custom name of this query, which can be used for logging
	// or referring to this particular query.
	Name string `json:"name,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The secret that contains prometheus credentials. Supports basic auth, bearer tokens, and TLS authentication.
	AuthSecret string `json:"authSecret,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Represents whether the metric type is Utilization, Value, or AverageValue.
	MetricType autoscalingv2.MetricTargetType `json:"metricType,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The detail about how to fetch metrics from Prometheus and scale workloads based on them
	Prometheus PrometheusSpec `json:"prometheus"`
}

type PrometheusSpec struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The URL of the Prometheus server.
	ServerAddress string `json:"serverAddress"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The PromQL query to fetch metrics (e.g., sum(vertica_sessions_running_counter{type="active", initiator="user"})).
	Query string `json:"query"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The threshold value at which scale up is triggered.
	Threshold int32 `json:"threshold"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// This is the lower bound at which the autoscaler starts scaling down to the minimum replica count.
	// If the metric falls below threshold but is still above this value, the current replica count remains unchanged.
	ScaleDownThreshold int32 `json:"scaleDownThreshold,omitempty"`
}

// MetricDefinition defines increment and metric to be used for autoscaling
type MetricDefinition struct {

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:Minimum:=0
	// The value used to increase the threshold after a scale up or a scale down.
	ThresholdAdjustmentValue int `json:"thresholdAdjustmentValue,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The threshold to use for scaling down. It must be of the same type as
	// the one used for scaling up, defined in the metric field.
	ScaleDownThreshold *autoscalingv2.MetricTarget `json:"scaleDownThreshold,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The custom metric to be used for autocaling.
	Metric autoscalingv2.MetricSpec `json:"metric,omitempty"`
}

type ScalingGranularityType string

const (
	PodScalingGranularity        = "Pod"
	SubclusterScalingGranularity = "Subcluster"
)

// VerticaAutoscalerStatus defines the observed state of VerticaAutoscaler
type VerticaAutoscalerStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The total number of times the operator has scaled up/down the VerticaDB.
	ScalingCount int `json:"scalingCount"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The observed size of all pods that are routed through the service name.
	CurrentSize int32 `json:"currentSize"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The selector used to find all of the pods for this autoscaler.
	Selector string `json:"selector"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Conditions for VerticaAutoscaler
	Conditions []VerticaAutoscalerCondition `json:"conditions,omitempty"`
}

// VerticaAutoscalerCondition defines condition for VerticaAutoscaler
type VerticaAutoscalerCondition struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Type is the type of the condition
	Type VerticaAutoscalerConditionType `json:"type"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Status is the status of the condition
	// can be True, False or Unknown
	Status corev1.ConditionStatus `json:"status"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type VerticaAutoscalerConditionType string

const (
	// TargetSizeInitialized indicates whether the operator has initialized targetSize in the spec
	TargetSizeInitialized VerticaAutoscalerConditionType = "TargetSizeInitialized"
	// ScalingActive indicates that the horizontal pod autoscaler can fetch the metric
	// and is ready for whenever scaling is needed.
	ScalingActive VerticaAutoscalerConditionType = "ScalingActive"
)

// Fixed index entries for each condition.
const (
	TargetSizeInitializedIndex = iota
	ScalingActiveIndex
)

var VasConditionIndexMap = map[VerticaAutoscalerConditionType]int{
	TargetSizeInitialized: TargetSizeInitializedIndex,
	ScalingActive:         ScalingActiveIndex,
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:categories=all;vertica,shortName=vas
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.targetSize,statuspath=.status.currentSize,selectorpath=.status.selector
//+kubebuilder:printcolumn:name="Granularity",type="string",JSONPath=".spec.scalingGranularity"
//+kubebuilder:printcolumn:name="Current Size",type="integer",JSONPath=".status.currentSize"
//+kubebuilder:printcolumn:name="Target Size",type="integer",JSONPath=".spec.targetSize"
//+kubebuilder:printcolumn:name="Scaling Count",type="integer",JSONPath=".status.scalingCount"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+operator-sdk:csv:customresourcedefinitions:resources={{VerticaDB,vertica.com/v1beta1,""}}

// VerticaAutoscaler is a CR that allows you to autoscale one or more
// subclusters in a VerticaDB.
type VerticaAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticaAutoscalerSpec   `json:"spec,omitempty"`
	Status VerticaAutoscalerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VerticaAutoscalerList contains a list of VerticaAutoscaler
type VerticaAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticaAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticaAutoscaler{}, &VerticaAutoscalerList{})
}

// MakeVASName is a helper that creates a sample name for test purposes
func MakeVASName() types.NamespacedName {
	return types.NamespacedName{Name: "vertica-vas-sample", Namespace: "default"}
}

// MakeVAS is a helper that constructs a fully formed VerticaAutoscaler struct using the sample name.
// This is intended for test purposes.
func MakeVAS() *VerticaAutoscaler {
	vasNm := MakeVASName()
	vdbNm := MakeVDBName()
	return &VerticaAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupVersion.String(),
			Kind:       VerticaAutoscalerKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        vasNm.Name,
			Namespace:   vasNm.Namespace,
			UID:         "abcdef-ghi",
			Annotations: make(map[string]string),
		},
		Spec: VerticaAutoscalerSpec{
			VerticaDBName:      vdbNm.Name,
			ScalingGranularity: PodScalingGranularity,
			ServiceName:        "sc1",
		},
	}
}

// MakeVASWithMetrics is a helper that constructs a fully formed VerticaAutoscaler struct with custom autoscaling enabled.
// This is intended for test purposes.
func MakeVASWithMetrics() *VerticaAutoscaler {
	vas := MakeVAS()
	minRep := int32(3)
	maxRep := int32(6)
	cpu := int32(80)
	vas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
		Hpa: &HPASpec{
			MinReplicas: &minRep,
			MaxReplicas: maxRep,
			Metrics: []MetricDefinition{
				{
					Metric: autoscalingv2.MetricSpec{
						Type: autoscalingv2.ResourceMetricSourceType,
						Resource: &autoscalingv2.ResourceMetricSource{
							Name: corev1.ResourceCPU,
							Target: autoscalingv2.MetricTarget{
								Type:               autoscalingv2.UtilizationMetricType,
								AverageUtilization: &cpu, // Scale when CPU exceeds 80%
							},
						},
					},
				},
			},
		},
	}
	return vas
}

// CanUseTemplate returns true if we can use the template provided in the spec
func (v *VerticaAutoscaler) CanUseTemplate() bool {
	return v.Spec.Template.Size > 0
}

// IsCustomMetricsEnabled returns true if the CR is set to use
// custom metrics for scaling.
func (v *VerticaAutoscaler) IsCustomMetricsEnabled() bool {
	return v.Spec.CustomAutoscaler != nil &&
		(v.Spec.CustomAutoscaler.Hpa != nil || v.Spec.CustomAutoscaler.ScaledObject != nil)
}
