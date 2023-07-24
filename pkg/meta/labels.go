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

package meta

const (
	SvcTypeLabel              = "vertica.com/svc-type"
	SubclusterNameLabel       = "vertica.com/subcluster-name"
	SubclusterLegacyNameLabel = "vertica.com/subcluster"
	SubclusterTypeLabel       = "vertica.com/subcluster-type"
	SubclusterSvcNameLabel    = "vertica.com/subcluster-svc"
	SubclusterTransientLabel  = "vertica.com/subcluster-transient"

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

	VDBInstanceLabel     = "app.kubernetes.io/instance"
	OperatorVersionLabel = "app.kubernetes.io/version"
	ManagedByLabel       = "app.kubernetes.io/managed-by"
	ComponentLabel       = "app.kubernetes.io/component"
	DataBaseLabel        = "vertica.com/database"

	NameLabel    = "app.kubernetes.io/name"
	OperatorName = "verticadb-operator" // The name of the operator

	CurOperatorVersion = "1.11.2" // The version number of the operator
	// If any of the operator versions are used in the code, add a const here.
	// But it isn't necessary to create a const for each version.
	OperatorVersion100 = "1.0.0"
	OperatorVersion110 = "1.1.0"
	OperatorVersion120 = "1.2.0"
	OperatorVersion130 = "1.3.0"
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
	SubclusterTransientLabel,
	SubclusterSvcNameLabel,
	SubclusterLegacyNameLabel,
	SvcTypeLabel,
	ClientRoutingLabel,
}
