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

//nolint:gosec
package events

// Constants for VerticaDB reconciler
const (
	AddNodeStart                     = "AddNodeStart"
	AddNodeSucceeded                 = "AddNodeSucceeded"
	AddNodeLicenseFail               = "AddNodeLicenseFail"
	AddNodeFailed                    = "AddNodeFailed"
	CreateDBStart                    = "CreateDBStart"
	CreateDBSucceeded                = "CreateDBSucceeded"
	CreateDBFailed                   = "CreateDBFailed"
	DropDBStart                      = "DropDBStart"
	DropDBSucceeded                  = "DropDBSucceeded"
	DropDBFailed                     = "DropDBFailed"
	ReviveDBStart                    = "ReviveDBStart"
	ReviveDBSucceeded                = "ReviveDBSucceeded"
	ReviveDBFailed                   = "ReviveDBFailed"
	ReviveDBClusterInUse             = "ReviveDBClusterInUse"
	ReviveDBNotFound                 = "ReviveDBNotFound"
	ReviveDBPermissionDenied         = "ReviveDBPermissionDenied"
	ReviveDBNodeCountMismatch        = "ReviveDBNodeCountMismatch"
	ReviveDBRestoreUnsupported       = "ReviveDBRestoreUnsupported"
	ReviveDBRestorePointNotFound     = "ReviveDBRestorePointNotFound"
	ReviveOrderBad                   = "ReviveOrderBad"
	ObjectNotFound                   = "ObjectNotFound"
	CommunalCredsWrongKey            = "CommunalCredsWrongKey"
	CommunalEndpointIssue            = "CommunalEndpointIssue"
	S3BucketDoesNotExist             = "S3BucketDoesNotExist"
	S3WrongRegion                    = "S3WrongRegion"
	S3SseCustomerWrongKey            = "S3SseCustomerWrongKey"
	AdditionalBucketsUpdated         = "AdditionalBucketsUpdated"
	InvalidS3SseCustomerKey          = "InvalidS3SseCustomerKey"
	InvalidConfigParm                = "InvalidConfigParm"
	CommunalPathIsNotEmpty           = "CommunalPathIsNotEmpty"
	RemoveNodesStart                 = "RemoveNodesStart"
	RemoveNodesSucceeded             = "RemoveNodesSucceeded"
	RemoveNodesFailed                = "RemoveNodesFailed"
	NodeRestartStarted               = "NodeRestartStarted"
	NodeRestartSucceeded             = "NodeRestartSucceeded"
	NodeRestartFailed                = "NodeRestartFailed"
	ClusterRestartStarted            = "ClusterRestartStarted"
	ClusterRestartSucceeded          = "ClusterRestartSucceeded"
	SandboxSubclusterFailed          = "SandboxSubclusterFailed"
	SandboxSubclusterStart           = "SandboxSubclusterStart"
	SandboxSubclusterSucceeded       = "SandboxSubclusterSucceeded"
	PromoteSandboxToMainFailed       = "PromoteSandboxSubclusterToMainFailed"
	PromoteSandboxToMainStart        = "PromoteSandboxSubclusterToMainStart"
	PromoteSandboxToSucceeded        = "PromoteSandboxSubclusterToMainSucceeded"
	UnsandboxSubclusterFailed        = "UnsandboxSubclusterFailed"
	UnsandboxSubclusterStart         = "UnsandboxSubclusterStart"
	UnsandboxSubclusterSucceeded     = "UnsandboxSubclusterSucceeded"
	CreateArchiveStart               = "CreateArchiveStart"
	CreateArchiveSucceeded           = "CreateArchiveSucceeded"
	ArchiveExists                    = "ArchiveExists"
	CreateArchiveFailed              = "CreateArchiveFailed"
	SaveRestorePointStart            = "SaveRestorePointStart"
	SaveRestorePointSucceeded        = "SaveRestorePointSucceeded"
	SaveRestorePointFailed           = "SaveRestorePointFailed"
	StopSubclusterStart              = "StopSubclusterStart"
	StopSubclusterSucceeded          = "StopSubclusterSucceeded"
	StopSubclusterFailed             = "StopSubclusterFailed"
	SlowRestartDetected              = "SlowRestartDetected"
	SubclusterAdded                  = "SubclusterAdded"
	RemoveSubcluserStart             = "RemoveSubcluserStart"
	SubclusterRemoved                = "SubclusterRemoved"
	RemoveSubclusterFailed           = "RemoveSubclusterFailed"
	AlterSubclusterFailed            = "AlterSubclusterFailed"
	AlterSubclusterSucceeded         = "AlterSubclusterSucceeded"
	SuperuserPasswordSecretNotFound  = "SuperuserPasswordSecretNotFound"
	UnsupportedVerticaVersion        = "UnsupportedVerticaVersion"
	ATConfPartiallyCopied            = "ATConfPartiallyCopied"
	AuthParmsCopyFailed              = "AuthParmsCopyFailed"
	UpgradeStart                     = "UpgradeStart"
	UpgradeSucceeded                 = "UpgradeSucceeded"
	IncompatibleUpgradeRequested     = "IncompatibleUpgradeRequested"
	ClusterShutdownStarted           = "ClusterShutdownStarted"
	ClusterShutdownFailed            = "ClusterShutdownFailed"
	ClusterShutdownSucceeded         = "ClusterShutdownSucceeded"
	ReipFailed                       = "ReipFailed"
	MissingSecretKeys                = "MissingSecretKeys"
	HTTPServerNotSetup               = "HTTPServerNotSetup"
	HTTPServerStartStarted           = "HTTPServerStartStarted"
	HTTPServerStartFailed            = "HTTPServerStartFailed"
	KerberosAuthError                = "KerberosAuthError"
	OperatorUpgrade                  = "OperatorUpgrade"
	InvalidUpgradePath               = "InvalidUpgradePath"
	RebalanceShards                  = "RebalanceShards"
	DrainNodeRetry                   = "DrainNodeRetry"
	DrainSubclusterRetry             = "DrainSubclusterRetry"
	DrainSubclusterTimeout           = "DrainSubclusterTimeout"
	SuboptimalNodeCount              = "SuboptimalNodeCount"
	StopDBStart                      = "StopDBStart"
	StopDBSucceeded                  = "StopDBSucceeded"
	StopDBFailed                     = "StopDBFailed"
	ClusterWillLoseQuorum            = "ClusterWillLoseQuorum"
	DepotResized                     = "DepotResized"
	MgmtFailed                       = "MgmtFailed"
	MgmtFailedDiskFull               = "MgmtFailedDiskfull"
	LowLocalDataAvailSpace           = "LowLocalDataAvailSpace"
	WrongImage                       = "WrongImage"
	MonolithicContainerNotSupported  = "MonolithicContainerNotSupported"
	InstallPackagesStarted           = "InstallPackagesStarted"
	InstallPackagesFailed            = "InstallPackagesFailed"
	InstallPackagesFinished          = "InstallPackagesFinished"
	RenameSubclusterFailed           = "RenameSubclusterFailed"
	RenameSubclusterStart            = "RenameSubclusterStart"
	RenameSubclusterSucceeded        = "RenameSubclusterSucceeded"
	InDBSaveRestorePointNotSupported = "InDBSaveRestorePointNotSupported"
	PauseConnectionsRetry            = "PauseConnectionsRetry"
	UpgradeFailed                    = "UpgradeFailed"
	NMATLSCertRotationStarted        = "NMATLSCertRotationStarted"
	NMATLSCertRotationSucceeded      = "NMATLSCertRotationSucceeded"
	HTTPSTLSUpdateStarted            = "HTTPSTLSUpdateStarted"
	HTTPSTLSUpdateSucceeded          = "HTTPSTLSUpdateSucceeded"
	HTTPSTLSUpdateFailed             = "HTTPSTLSUpdateFailed"
	ClientServerTLSUpdateStarted     = "ClientServerTLSUpdateStarted"
	ClientServerTLSUpdateSucceeded   = "ClientServerTLSUpdateSucceeded"
	ClientServerTLSUpdateFailed      = "ClientServerTLSUpdateFailed"
	TLSConfigurationStarted          = "TLSConfigurationStarted"
	TLSConfigurationSucceeded        = "TLSConfigurationSucceeded"
	TLSConfigurationFailed           = "TLSConfigurationFailed"
	TLSModeUpdateStarted             = "TLSModeUpdateStarted"
	TLSModeUpdateSucceeded           = "TLSModeUpdateSucceeded"
	TLSCertValidationFailed          = "TLSCertValidationFailed"
)

// Constants for VerticaAutoscaler reconciler
const (
	SubclusterServiceNameNotFound = "SubclusterServiceNameNotFound"
	VerticaDBNotFound             = "VerticaDBNotFound"
	NoSubclusterTemplate          = "NoSubclusterTemplate"
	PrometheusMetricsNotSupported = "PrometheusMetricsNotSupported"
)

// Constants for VerticaScrutinize reconciler
const (
	VclusterOpsDisabled               = "VclusterOpsDisabled"
	VerticaVersionNotFound            = "VerticaVersionNotFound"
	VclusterOpsScrutinizeNotSupported = "VclusterOpsScrutinizeNotSupported"
	VclusterOpsScrutinizeSucceeded    = "VclusterOpsScrutinizeSucceeded"
	VclusterOpsScrutinizeFailed       = "VclusterOpsScrutinizeFailed"
	SandboxNotFound                   = "SandboxNotFound"
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
