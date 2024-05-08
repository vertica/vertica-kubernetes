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
	ReviveDBRestoreUnsupported      = "ReviveDBRestoreUnsupported"
	ReviveDBRestorePointNotFound    = "ReviveDBRestorePointNotFound"
	ReviveOrderBad                  = "ReviveOrderBad"
	ObjectNotFound                  = "ObjectNotFound"
	CommunalCredsWrongKey           = "CommunalCredsWrongKey" //nolint:gosec
	CommunalEndpointIssue           = "CommunalEndpointIssue"
	S3BucketDoesNotExist            = "S3BucketDoesNotExist"
	S3WrongRegion                   = "S3WrongRegion"
	S3SseCustomerWrongKey           = "S3SseCustomerWrongKey"
	InvalidS3SseCustomerKey         = "InvalidS3SseCustomerKey"
	InvalidConfigParm               = "InvalidConfigParm"
	CommunalPathIsNotEmpty          = "CommunalPathIsNotEmpty"
	RemoveNodesStart                = "RemoveNodesStart"
	RemoveNodesSucceeded            = "RemoveNodesSucceeded"
	RemoveNodesFailed               = "RemoveNodesFailed"
	NodeRestartStarted              = "NodeRestartStarted"
	NodeRestartSucceeded            = "NodeRestartSucceeded"
	NodeRestartFailed               = "NodeRestartFailed"
	ClusterRestartStarted           = "ClusterRestartStarted"
	ClusterRestartSucceeded         = "ClusterRestartSucceeded"
	SandboxSubclusterFailed         = "SandboxSubclusterFailed"
	SandboxSubclusterStart          = "SandboxSubclusterStart"
	SandboxSubclusterSucceeded      = "SandboxSubclusterSucceeded"
	UnsandboxSubclusterFailed       = "UnsandboxSubclusterFailed"
	UnsandboxSubclusterStart        = "UnsandboxSubclusterStart" //nolint:gosec
	UnsandboxSubclusterSucceeded    = "UnsandboxSubclusterSucceeded"
	SlowRestartDetected             = "SlowRestartDetected"
	SubclusterAdded                 = "SubclusterAdded"
	SubclusterRemoved               = "SubclusterRemoved"
	SuperuserPasswordSecretNotFound = "SuperuserPasswordSecretNotFound"
	UnsupportedVerticaVersion       = "UnsupportedVerticaVersion"
	ATConfPartiallyCopied           = "ATConfPartiallyCopied"
	AuthParmsCopyFailed             = "AuthParmsCopyFailed"
	UpgradeStart                    = "UpgradeStart"
	UpgradeSucceeded                = "UpgradeSucceeded"
	IncompatibleUpgradeRequested    = "IncompatibleUpgradeRequested"
	ClusterShutdownStarted          = "ClusterShutdownStarted"
	ClusterShutdownFailed           = "ClusterShutdownFailed"
	ClusterShutdownSucceeded        = "ClusterShutdownSucceeded"
	ReipFailed                      = "ReipFailed"
	MissingSecretKeys               = "MissingSecretKeys"
	HTTPServerNotSetup              = "HTTPServerNotSetup"
	HTTPServerStartStarted          = "HTTPServerStartStarted"
	HTTPServerStartFailed           = "HTTPServerStartFailed"
	KerberosAuthError               = "KerberosAuthError"
	OperatorUpgrade                 = "OperatorUpgrade"
	InvalidUpgradePath              = "InvalidUpgradePath"
	RebalanceShards                 = "RebalanceShards"
	DrainNodeRetry                  = "DrainNodeRetry"
	DrainSubclusterRetry            = "DrainSubclusterRetry"
	SuboptimalNodeCount             = "SuboptimalNodeCount"
	StopDBStart                     = "StopDBStart"
	StopDBSucceeded                 = "StopDBSucceeded"
	StopDBFailed                    = "StopDBFailed"
	DepotResized                    = "DepotResized"
	MgmtFailed                      = "MgmtFailed"
	MgmtFailedDiskFull              = "MgmtFailedDiskfull"
	LowLocalDataAvailSpace          = "LowLocalDataAvailSpace"
	WrongImage                      = "WrongImage"
	MonolithicContainerNotSupported = "MonolithicContainerNotSupported"
	InstallPackagesStarted          = "InstallPackagesStarted"
	InstallPackagesFailed           = "InstallPackagesFailed"
	InstallPackagesFinished         = "InstallPackagesFinished"
)

// Constants for VerticaAutoscaler reconciler
const (
	SubclusterServiceNameNotFound = "SubclusterServiceNameNotFound"
	VerticaDBNotFound             = "VerticaDBNotFound"
	NoSubclusterTemplate          = "NoSubclusterTemplate"
)

// Constants for VerticaScrutinize reconciler
const (
	VclusterOpsDisabled               = "VclusterOpsDisabled"
	VerticaVersionNotFound            = "VerticaVersionNotFound"
	VclusterOpsScrutinizeNotSupported = "VclusterOpsScrutinizeNotSupported"
	VclusterOpsScrutinizeSucceeded    = "VclusterOpsScrutinizeSucceeded"
	VclusterOpsScrutinizeFailed       = "VclusterOpsScrutinizeFailed"
)

// Constants for VerticaReplicator reconciler
const (
	ReplicationNotSupported    = "ReplicationNotSupported"
	VrepAdmintoolsNotSupported = "AdmintoolsNotSupported"
	ReplicationStarted         = "ReplicationStarted"
	ReplicationFailed          = "ReplicationFailed"
	ReplicationSucceeded       = "ReplicationSucceeded"
)

// Constants for VerticaRestorePointsQuery reconciler
const (
	RestoreNotSupported        = "RestoreNotSupported"
	VrpqAdmintoolsNotSupported = "AdmintoolsNotSupported"
	ShowRestorePointsStarted   = "ShowRestorePointsStarted"
	ShowRestorePointsFailed    = "ShowRestorePointsFailed"
	ShowRestorePointsSucceeded = "ShowRestorePointsSucceeded"
)

// Constants for sandbox ConfigMap reconciler
const (
	SandboxNotSupported = "SandboxNotSupported"
)
