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
package v1

import (
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// VerticaAutoscalerSpec defines the desired state of VerticaAutoscaler
type VerticaAutoscalerSpec struct {
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The name of the VerticaDB CR that this autoscaler is defined for.  The
	// VerticaDB object must exist in the same namespace as this object.
	VerticaDBName string `json:"verticaDBName"`

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

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// This acts as a selector for the subclusters that are being scaled together.
	// Each subcluster has a service name field, which if omitted is the same
	// name as the subcluster name.  Multiple subclusters that have the same
	// service name use the same service object.
	// if this field is empty, all the subclusters will be selected for scaling.
	ServiceName string `json:"serviceName,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:fieldDependency:scalingGranularity:Subcluster"
	// When the scaling granularity is Subcluster, this field defines a template
	// to use for when a new subcluster needs to be created.  If size is 0, then
	// the operator will use an existing subcluster to use as the template.  If
	// size is > 0 service name must match the serviceName parameter (if non-empty).
	//
	// If the serviceName parameter is empty, service name can be an existing service and
	// in that case the new subcluster will share it with other(s) subcluster, service
	// name can also be non-existing and all the subclusters created from the template
	// will share that service. If service name is empty, each new subcluster will have its
	// own service.
	//
	// The name of the new subcluster is always auto generated.  If the name is set
	// here it will be used as a prefix for the new subcluster.  Otherwise, we
	// use the name of this VerticaAutoscaler object as a prefix for all
	// subclusters.
	Template Subcluster `json:"template"`

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
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=ScaledObject
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:HPA","urn:alm:descriptor:com.tectonic.ui:select:ScaledObject"}
	// The type of autoscaler. It must be one of "HPA" or "ScaledObject".
	Type string `json:"type,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:fieldDependency:customAutoscaler.type:HPA"
	// It refers to an autoscaling definition through the horizontal pod autoscaler.
	// If type is "HPA", this must be set.
	Hpa *HPASpec `json:"hpa,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:fieldDependency:customAutoscaler.type:ScaledObject"
	// It refers to an autoscaling definition through a scaledObject.
	// If type is "ScaledObject", this must be set.
	ScaledObject *ScaledObjectSpec `json:"scaledObject,omitempty"`
}

const (
	HPA          = "HPA"
	ScaledObject = "ScaledObject"
)

type HPASpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:Minimum:=0
	// +kubebuilder:default:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// The miminum number of pods when scaling.
	MinReplicas *int32 `json:"minReplicas"`

	// +kubebuilder:validation:Required
	// +kubebuilder:Minimum:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// The maximum number of pods when scaling.
	MaxReplicas int32 `json:"maxReplicas"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The custom metric definition to be used for autocaling.
	Metrics []MetricDefinition `json:"metrics,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Specifies the scaling behavior for both scale out and in.
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behavior,omitempty"`
}

type ScaledObjectSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:Minimum:=0
	// +kubebuilder:default:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// The miminum number of pods when scaling.
	MinReplicas *int32 `json:"minReplicas"`

	// +kubebuilder:validation:Required
	// +kubebuilder:Minimum:=1
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// The maximum number of pods when scaling.
	MaxReplicas *int32 `json:"maxReplicas"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=30
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// The time interval at which the scaler will check the metric condition and scale the target (in seconds).
	// If not specified, the default is 30 seconds.
	PollingInterval *int32 `json:"pollingInterval,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=30
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// Defines the time to wait between scaling actions. This is helpful to avoid constant scaling out/in. Default: 30s.
	CooldownPeriod *int32 `json:"cooldownPeriod,omitempty"`

	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The list of prometheus queries that will be used for scaling.
	Metrics []ScaleTrigger `json:"metrics"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Specifies the scaling behavior for both scale out and in.
	Behavior *autoscalingv2.HorizontalPodAutoscalerBehavior `json:"behavior,omitempty"`
}

type ScaleTrigger struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:prometheus","urn:alm:descriptor:com.tectonic.ui:select:cpu","urn:alm:descriptor:com.tectonic.ui:select:memory"}
	// The type of metric that is being defined. It can be either cpu, memory, or prometheus.
	// An empty string currently defaults prometheus.
	Type TriggerType `json:"type,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The custom name of this metric, which can be used for logging
	// or referring to this particular metric.
	Name string `json:"name,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	// The secret that contains prometheus credentials. Supports basic auth, bearer tokens, and TLS authentication.
	// It will ignored if the type is not prometheus
	AuthSecret string `json:"authSecret,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Utilization","urn:alm:descriptor:com.tectonic.ui:select:Value","urn:alm:descriptor:com.tectonic.ui:select:AverageValue"}
	// Represents whether the metric type is Utilization, Value, or AverageValue.
	// Allowed types are 'Value' or 'AverageValue' for prometheus and
	// 'Utilization' or 'AverageValue' for cpu/memory. If not specified, it defaults to Value
	// for prometheus and Utilization for cpu/memory.
	MetricType autoscalingv2.MetricTargetType `json:"metricType,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:fieldDependency:customAutoscaler.scaledObject.metrics[0].type:prometheus"
	// The detail about how to fetch metrics from Prometheus and scale workloads based on them.
	// if type is "prometheus", this must be set.
	Prometheus *PrometheusSpec `json:"prometheus,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:fieldDependency:customAutoscaler.scaledObject.metrics[0].type:cpu","urn:alm:descriptor:com.tectonic.ui:fieldDependency:customAutoscaler.scaledObject.metrics[0].type:memory"}
	// The detail about the target value and container name. if type is cpu/memory
	// this must be set.
	Resource *CPUMemorySpec `json:"resource,omitempty"`
}

type TriggerType string

const (
	CPUTriggerType        TriggerType = "cpu"
	MemTriggerType        TriggerType = "memory"
	PrometheusTriggerType TriggerType = "prometheus"
)

type PrometheusAuthModes string

const (
	PrometheusAuthBasic       PrometheusAuthModes = "basic"
	PrometheusAuthBearer      PrometheusAuthModes = "bearer"
	PrometheusAuthTLS         PrometheusAuthModes = "tls"
	PrometheusAuthCustom      PrometheusAuthModes = "custom"
	PrometheusAuthTLSAndBasic PrometheusAuthModes = "tls,basic"
)

const (
	PrometheusSecretKeyUsername         string = "username"
	PrometheusSecretKeyPassword         string = "password"
	PrometheusSecretKeyBearerToken      string = "bearerToken"
	PrometheusSecretKeyCa               string = "ca"
	PrometheusSecretKeyCert             string = "cert"
	PrometheusSecretKeyKey              string = "key"
	PrometheusSecretKeyCustomAuthHeader string = "customAuthHeader"
	PrometheusSecretKeyCustomAuthValue  string = "customAuthValue"
)

type PrometheusSpec struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The URL of the Prometheus server.
	ServerAddress string `json:"serverAddress"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The PromQL query to fetch metrics (e.g., sum(vertica_sessions_running_counter{type="active", initiator="user"})).
	Query string `json:"query"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// The threshold value at which scale out is triggered.
	Threshold int32 `json:"threshold"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// This is the lower bound at which the autoscaler starts scaling in to the minimum replica count.
	// If the metric falls below threshold but is still above this value, the current replica count remains unchanged.
	ScaleInThreshold int32 `json:"scaleInThreshold,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:basic","urn:alm:descriptor:com.tectonic.ui:select:bearer","urn:alm:descriptor:com.tectonic.ui:select:tls","urn:alm:descriptor:com.tectonic.ui:select:custom","urn:alm:descriptor:com.tectonic.ui:select:tls,basic"}
	// The authentication methods for Prometheus.
	// Allowed types are 'basic', 'bearer', 'tls', 'custom' and 'tls,basic'.
	// For 'basic' type, 'username' and 'password' are required fields in AuthSecret.
	// For 'bearer' type, 'bearerToken' is required field in AuthSecret.
	// For 'tls' type, 'ca', 'cert' and 'key' are required fields in AuthSecret.
	// For 'custom' type, 'customAuthHeader' and 'customAuthValue' are required fields in AuthSecret.
	// For 'tls,basic' type, 'username', 'password', 'ca', 'cert' and 'key' are required fields in AuthSecret.
	AuthModes PrometheusAuthModes `json:"authModes,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	// Used for skipping certificate check e.g: using self-signed certs.
	UnsafeSsl bool `json:"unsafeSsl,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	// Enables caching of metric values during polling interval. It is Used to control whether the autoscaler should use cached metrics for scaling
	// decisions rather than querying the external metric provider (e.g., Prometheus) on each scale event. This feature is not supported for cpu and memory.
	UseCachedMetrics bool `json:"useCachedMetrics,omitempty"`
}

type CPUMemorySpec struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// The value to trigger scaling for.
	//
	// - When using Utilization, the target value is the average of the resource metric across all relevant pods,
	// 	 represented as a percentage of the requested value of the resource for the pods.
	// - When using AverageValue, the target value is the target value of the average of the metric
	//   across all relevant pods (quantity).
	Threshold int32 `json:"threshold"`
}

// MetricDefinition defines increment and metric to be used for autoscaling
type MetricDefinition struct {

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The threshold to use for scaling in. It must be of the same type as
	// the one used for scaling up, defined in the metric field.
	ScaleInThreshold *autoscalingv2.MetricTarget `json:"scaleInThreshold,omitempty"`

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
	// The total number of times the operator has scaled out/in the VerticaDB.
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
	// ScalingActive indicates that the autoscaler can fetch the metric
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

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:conversion:hub
// +kubebuilder:resource:categories=all;vertica,shortName=vas
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.targetSize,statuspath=.status.currentSize,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Granularity",type="string",JSONPath=".spec.scalingGranularity"
// +kubebuilder:printcolumn:name="Current Size",type="integer",JSONPath=".status.currentSize"
// +kubebuilder:printcolumn:name="Target Size",type="integer",JSONPath=".spec.targetSize"
// +kubebuilder:printcolumn:name="Scaling Count",type="integer",JSONPath=".status.scalingCount"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +operator-sdk:csv:customresourcedefinitions:resources={{VerticaDB,vertica.com/v1,""},{ScaledObject,keda.sh/v1alpha1,""},{TriggerAuthentication,keda.sh/v1alpha1,""}}

// VerticaAutoscaler is a CR that allows you to autoscale one or more
// subclusters in a VerticaDB.
type VerticaAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticaAutoscalerSpec   `json:"spec,omitempty"`
	Status VerticaAutoscalerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

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
		Type: HPA,
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

// MakeVASWithScaledObject is a helper that constructs a fully formed VerticaAutoscaler struct with custom autoscaling enabled.
// This is intended for test purposes.
func MakeVASWithScaledObject() *VerticaAutoscaler {
	vas := MakeVAS()
	minRep := int32(3)
	maxRep := int32(6)
	threshold := int32(50)
	vas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
		Type: ScaledObject,
		ScaledObject: &ScaledObjectSpec{
			MinReplicas: &minRep,
			MaxReplicas: &maxRep,
			Metrics: []ScaleTrigger{
				{
					Type:       CPUTriggerType,
					MetricType: autoscalingv2.AverageValueMetricType,
					Resource: &CPUMemorySpec{
						Threshold: threshold,
					},
				},
			},
		},
	}
	return vas
}

// MakeVASWithScaledObjectPrometheus is a helper that constructs a fully formed VerticaAutoscaler struct with Prometheus.
// This is intended for test purposes.
func MakeVASWithScaledObjectPrometheus() *VerticaAutoscaler {
	vas := MakeVAS()
	minRep := int32(3)
	maxRep := int32(6)
	vas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
		Type: ScaledObject,
		ScaledObject: &ScaledObjectSpec{
			MinReplicas: &minRep,
			MaxReplicas: &maxRep,
			Metrics: []ScaleTrigger{
				{
					Type:       PrometheusTriggerType,
					Prometheus: &PrometheusSpec{},
					MetricType: autoscalingv2.AverageValueMetricType,
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

// IsCustomAutoScalerSet returns true if the customAutoscaler field is set.
func (v *VerticaAutoscaler) IsCustomAutoScalerSet() bool {
	return v.Spec.CustomAutoscaler != nil && v.Spec.CustomAutoscaler.Type != ""
}

// IsCustomMetricsEnabled returns true if the CR is set to use
// custom metrics for scaling.
func (v *VerticaAutoscaler) IsCustomMetricsEnabled() bool {
	return v.IsCustomAutoScalerSet() &&
		(v.Spec.CustomAutoscaler.Hpa != nil || v.Spec.CustomAutoscaler.ScaledObject != nil)
}

// IsHpaEnabled returns true if custom autoscaling with hpa is set.
func (v *VerticaAutoscaler) IsHpaEnabled() bool {
	return v.IsCustomAutoScalerSet() && v.Spec.CustomAutoscaler.Type == HPA && v.Spec.CustomAutoscaler.Hpa != nil
}

// IsHpaType returns true if custom autoscaler type is HPA.
func (v *VerticaAutoscaler) IsHpaType() bool {
	return v.IsCustomAutoScalerSet() && v.Spec.CustomAutoscaler.Type == HPA
}

// IsScaledObjectEnabled returns true if custom autoscaling with scaledObject is set.
func (v *VerticaAutoscaler) IsScaledObjectEnabled() bool {
	return v.IsCustomAutoScalerSet() && v.Spec.CustomAutoscaler.Type == ScaledObject && v.Spec.CustomAutoscaler.ScaledObject != nil
}

// IsScaledObjectType returns true if custom autoscaler type is SacledObject.
func (v *VerticaAutoscaler) IsScaledObjectType() bool {
	return v.IsCustomAutoScalerSet() && v.Spec.CustomAutoscaler.Type == ScaledObject
}
