/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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
	S3CredsNotFound                 = "S3CredsNotFound"
	S3CredsWrongKey                 = "S3CredsWrongKey"
	S3EndpointIssue                 = "S3EndpointIssue"
	S3BucketDoesNotExist            = "S3BucketDoesNotExist"
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
	S3AuthParmsCopyFailed           = "S3AuthParmsCopyFailed"
)
