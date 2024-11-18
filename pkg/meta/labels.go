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

package meta

const (
	// Labels used for all object types
	//
	// This is a label that is set for all objects. It includes the name of the
	// VerticaDB owning the object.
	VDBInstanceLabel = "app.kubernetes.io/instance"
	//
	// This is a label that is set for all objects. It includes the name of the
	// operator. It always is set to the value of OperatorName.
	ManagedByLabel = "app.kubernetes.io/managed-by"
	OperatorName   = "verticadb-operator" // The name of the operator
	//
	// This is a label that is set for all objects. It is a standard k8s label
	// to indicate that this operator is for a database.
	ComponentLabel    = "app.kubernetes.io/component"
	ComponentDatabase = "database"
	//
	// This is a label that is set for all objects. It includes the name of the
	// VerticaDB database that the CR is for.
	DataBaseLabel = "vertica.com/database"
	//
	// This is a label that is set for all objects. We set it to the NameValue.
	// However, it can be overridden by annotations set in the CR.
	NameLabel = "app.kubernetes.io/name"
	NameValue = "vertica"
	//
	// This is a label that is set for all objects. For pods this isn't set in
	// the pod spec template. Rather it is set in the pod by a reconciler after
	// the pod was created.
	OperatorVersionLabel = "app.kubernetes.io/version"
	CurOperatorVersion   = "24.4.0-0" // The version number of the operator
	// If any of the operator versions are used in the code, add a const here.
	// But it isn't necessary to create a const for each version.
	OperatorVersion100 = "1.0.0"
	OperatorVersion220 = "2.2.0"

	// Service objects
	//
	// This defines what type of service it is for. The values for this label
	// immediately follow it.
	SvcTypeLabel    = "vertica.com/svc-type"
	SvcTypeExternal = "external"
	SvcTypeHeadless = "headless"

	// Statefulset + Service objects
	//
	// The name of the subcluster this object is a part of.
	SubclusterNameLabel = "vertica.com/subcluster-name"
	//
	// Prior to 1.3.0, we had a different label to denote the the subcluster
	// name.  We renamed it as we added additional subcluster attributes to the
	// label. This is kept here for legacy purposes.
	SubclusterLegacyNameLabel = "vertica.com/subcluster"
	//
	// The type of the subcluster. Values are can be either: primary or
	// secondary.
	SubclusterTypeLabel = "vertica.com/subcluster-type"
	//
	// This label is added to a statefulset to indicate the sandbox it belongs
	// to. This will allow the operator to filter these objects if it is looking
	// for objects from the main cluster or a sandbox.  Moreover, the sandbox
	// controller will be watching statefulsets with this label and will trigger
	// a reconcile loop if it finds a configmap with a sandbox name equal to
	// this label's value
	SandboxNameLabel = "vertica.com/sandbox"

	// Pod objects
	//
	// The name of the service object to use for the pod. Note, if the
	// spec.subclusters[].serviceName changes during a deployment, then the pods
	// will be restarted for it to take effect.
	SubclusterSvcNameLabel = "vertica.com/subcluster-svc"
	//
	// ClientRoutingLabel is a label that must exist on the pod in
	// order for Service objects to route to the pod.  This label isn't part of
	// the template in the StatefulSet.  This label is added after the pod is
	// scheduled.  There are a couple of uses for it:
	// - after an add node, we only add the labels once the node has at least
	// one shard subscription.  This saves routing to a pod that cannot fulfill
	// a query request.
	// - before we remove a node.  It allows us to drain out pods that are going
	// to be removed by a pending node removal.
	ClientRoutingLabel = "vertica.com/client-routing"
	ClientRoutingVal   = "true"
	//
	// This is set in the pods, and is used by statefulset as a pod selector. This
	// stays constant for the life of the statefulset, and is unique across
	// statefulsets for other subclusters in the database. It may appear like it
	// is derived from a subcluster name, but is does not get updated for
	// subcluster rename. So, it shouldn't be treated as such.
	SubclusterSelectorLabel = "vertica.com/subcluster-selector-name"

	// This is set in the pods, and is used by deployment as a pod selector. This
	// stays constant for the life of the deployment, and is unique across
	// deployment for other subclusters in the database. It may appear like it
	// is derived from a subcluster name, but is does not get updated for
	// subcluster rename. So, it shouldn't be treated as such.
	DeploymentSelectorLabel = "vertica.com/deployment-selector-name"

	// This is set in all proxy pods, and is used to filter out all proxy pods
	// in vdb reconcilers as a pod selector. This stays constant for the life
	// of the deployment. If it is set to true, then the pod is a proxy pod;
	// if it is set to other value or not set, then the pod is not a proxy pod.
	ProxyPodSelectorLabel = "vertica.com/proxy-pod"
	ProxyPodSelectorVal   = "true"

	// ConfigMap objects
	//
	// This indicates that the object is watched by the sandbox controller.
	// It must be set in configmaps that carry the a sandbox state or statefulsets
	// that represent sandboxed subclusters
	WatchedBySandboxLabel = "vertica.com/watched-by-sandbox-controller"
	WatchedBySandboxTrue  = "true"

	// This indicates that the object is used as a client proxy object.
	ClientProxyLabel = "vertica.com/client-porxy"
	ClientProxyTrue  = "true"
)

// ProtectedLabels lists all of the internally used label.
var ProtectedLabels = []string{
	ManagedByLabel,
	VDBInstanceLabel,
	ComponentLabel,
	OperatorVersionLabel,
	DataBaseLabel,
	SubclusterNameLabel,
	SubclusterTypeLabel,
	SubclusterSvcNameLabel,
	SubclusterLegacyNameLabel,
	SvcTypeLabel,
	ClientRoutingLabel,
}

var SandboxConfigMapLabels = []string{
	ManagedByLabel,
	VDBInstanceLabel,
	ComponentLabel,
	DataBaseLabel,
	NameLabel,
	WatchedBySandboxLabel,
}
