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

package events

// Constants for VerticaDB reconciler
const (
	AddNodeStart                    = "AddNodeStart"
	AddNodeSucceeded                = "AddNodeSucceeded"
	AddNodeLicenseFail              = "AddNodeLicenseFail"
	AddNodeFailed                   = "AddNodeFailed"
	CreateDBStart                   = "CreateDBStart"
	CreateDBSucceeded               = "CreateDBSucceeded"
	CreateDBFailed                  = "CreateDBFailed"
	ReviveDBStart                   = "ReviveDBStart"
	ReviveDBSucceeded               = "ReviveDBSucceeded"
	ReviveDBFailed                  = "ReviveDBFailed"
	ReviveDBClusterInUse            = "ReviveDBClusterInUse"
	ReviveDBNotFound                = "ReviveDBNotFound"
	ReviveDBPermissionDenied        = "ReviveDBPermissionDenied"
	ReviveDBNodeCountMismatch       = "ReviveDBNodeCountMismatch"
	ReviveOrderBad                  = "ReviveOrderBad"
	ObjectNotFound                  = "ObjectNotFound"
	CommunalCredsWrongKey           = "CommunalCredsWrongKey"
	S3EndpointIssue                 = "S3EndpointIssue"
	S3BucketDoesNotExist            = "S3BucketDoesNotExist"
	S3WrongRegion                   = "S3WrongRegion"
	CommunalPathIsNotEmpty          = "CommunalPathIsNotEmpty"
	RemoveNodesStart                = "RemoveNodesStart"
	RemoveNodesSucceeded            = "RemoveNodesSucceeded"
	RemoveNodesFailed               = "RemoveNodesFailed"
	NodeRestartStarted              = "NodeRestartStarted"
	NodeRestartFailed               = "NodeRestartFailed"
	NodeRestartSucceeded            = "NodeRestartSucceeded"
	ClusterRestartStarted           = "ClusterRestartStarted"
	ClusterRestartFailed            = "ClusterRestartFailed"
	ClusterRestartSucceeded         = "ClusterRestartSucceeded"
	SubclusterAdded                 = "SubclusterAdded"
	SubclusterRemoved               = "SubclusterRemoved"
	SuperuserPasswordSecretNotFound = "SuperuserPasswordSecretNotFound"
	UnsupportedVerticaVersion       = "UnsupportedVerticaVersion"
	ATConfPartiallyCopied           = "ATConfPartiallyCopied"
	AuthParmsCopyFailed             = "AuthParmsCopyFailed"
	UpgradeStart                    = "UpgradeStart"
	UpgradeSucceeded                = "UpgradeSucceeded"
	IncompatibleOnlineUpgrade       = "IncompatibleOnlineUpgrade"
	ClusterShutdownStarted          = "ClusterShutdownStarted"
	ClusterShutdownFailed           = "ClusterShutdownFailed"
	ClusterShutdownSucceeded        = "ClusterShutdownSucceeded"
	ReipFailed                      = "ReipFailed"
	MissingSecretKeys               = "MissingSecretKeys"
	KerberosAuthError               = "KerberosAuthError"
	OperatorUpgrade                 = "OperatorUpgrade"
	InvalidUpgradePath              = "InvalidUpgradePath"
	RebalanceShards                 = "RebalanceShards"
	DrainNodeRetry                  = "DrainNodeRetry"
	SuboptimalNodeCount             = "SuboptimalNodeCount"
)

// Constants for VerticaAutoscaler reconciler
const (
	SubclusterServiceNameNotFound = "SubclusterServiceNameNotFound"
	VerticaDBNotFound             = "VerticaDBNotFound"
	NoSubclusterTemplate          = "NoSubclusterTemplate"
)
