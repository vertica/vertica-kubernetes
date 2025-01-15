/*
 (c) Copyright [2023-2024] Open Text.
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

package rfc7807

import (
	"net/http"
	"path"
)

// List of all known RFC 7807 problems that vcluster may see. All are exported
// from the package so they can be used by the NMA, vcluster, etc.
//
// Treat each problem's type (URL reference) as immutable. They should never be
// changed across server releases. And they should never be reused as it is used to
// uniquely identify the problem that is hit.
//
// In general, the title should be constant too. The only time we may want to
// relax that is if they are changed for localization purposes.
const errorEndpointsPrefix = "https://integrators.vertica.com/rest/errors/"

var (
	GenericBootstrapCatalogFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "internal-bootstrap-catalog-failure"),
		"Internal error while bootstraping the catalog",
		http.StatusInternalServerError,
	)
	CommunalStorageNotEmpty = newProblemID(
		path.Join(errorEndpointsPrefix, "communal-storage-not-empty"),
		"Communal storage is not empty",
		http.StatusInternalServerError,
	)
	CommunalStoragePathInvalid = newProblemID(
		path.Join(errorEndpointsPrefix, "communal-storage-path-invalid"),
		"Communal storage is not a valid path for the file system",
		http.StatusInternalServerError,
	)
	CommunalRWAccessError = newProblemID(
		path.Join(errorEndpointsPrefix, "communal-read-write-access-error"),
		"Failed while testing read/write access to the communal storage",
		http.StatusInternalServerError,
	)
	CommunalAccessError = newProblemID(
		path.Join(errorEndpointsPrefix, "communal-access-error"),
		"Error accessing communal storage",
		http.StatusInternalServerError,
	)
	GenericLicenseCheckFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "internal-license-check-failure"),
		"Internal error while checking license file",
		http.StatusInternalServerError,
	)
	WrongRequestMethod = newProblemID(
		path.Join(errorEndpointsPrefix, "wrong-request-method"),
		"Wrong request method used",
		http.StatusMethodNotAllowed,
	)
	BadRequest = newProblemID(
		path.Join(errorEndpointsPrefix, "bad-request"),
		"Bad request sent",
		http.StatusBadRequest,
	)
	GenericHTTPInternalServerError = newProblemID(
		path.Join(errorEndpointsPrefix, "http-internal-server-error"),
		"Internal server error",
		http.StatusInternalServerError,
	)
	GenericGetNodeInfoFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "internal-get-node-info-failure"),
		"Internal error while getting node information",
		http.StatusInternalServerError,
	)
	GenericLoadRemoteCatalogFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "internal-load-remote-catalog-failure"),
		"Internal error while loading remote catalog",
		http.StatusInternalServerError,
	)
	GenericSpreadSecurityPersistenceFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "spread-security-persistence-failure"),
		"Internal error while persisting spread encryption key",
		http.StatusInternalServerError,
	)
	GenericShowRestorePointsFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "internal-show-restore-points-failure"),
		"Internal error while showing restore points",
		http.StatusInternalServerError,
	)
	SubclusterNotFound = newProblemID(
		path.Join(errorEndpointsPrefix, "subcluster-not-found"),
		"Subcluster is not found",
		http.StatusInternalServerError,
	)
	GenericCatalogEditorFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "internal-catalog-editor-failure"),
		"Internal error while running catalog editor",
		http.StatusInternalServerError,
	)
	GenericVerticaDownloadFileFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "general-vertica-download-file-failure"),
		"General error while running Vertica download file",
		http.StatusInternalServerError,
	)
	InsufficientPrivilege = newProblemID(
		path.Join(errorEndpointsPrefix, "insufficient-privilege"),
		"Insufficient privilege",
		http.StatusInternalServerError,
	)
	UndefinedFile = newProblemID(
		path.Join(errorEndpointsPrefix, "undefined-file"),
		"Undefined file",
		http.StatusInternalServerError,
	)
	DuplicateFile = newProblemID(
		path.Join(errorEndpointsPrefix, "duplicate-file"),
		"Duplicate file",
		http.StatusInternalServerError,
	)
	WrongObjectType = newProblemID(
		path.Join(errorEndpointsPrefix, "wrong-object-type"),
		"Wrong object type",
		http.StatusInternalServerError,
	)
	DiskFull = newProblemID(
		path.Join(errorEndpointsPrefix, "disk-full"),
		"Disk full",
		http.StatusInternalServerError,
	)
	InsufficientResources = newProblemID(
		path.Join(errorEndpointsPrefix, "insufficient-resources"),
		"Insufficient resources",
		http.StatusInternalServerError,
	)
	IOError = newProblemID(
		path.Join(errorEndpointsPrefix, "io-error"),
		"IO error",
		http.StatusInternalServerError,
	)
	QueryCanceled = newProblemID(
		path.Join(errorEndpointsPrefix, "query-canceled"),
		"Query canceled",
		http.StatusInternalServerError,
	)
	InternalVerticaDownloadFileFailure = newProblemID(
		path.Join(errorEndpointsPrefix, "internal-vertica-download-file-failure"),
		"Internal error while running Vertica download file",
		http.StatusInternalServerError,
	)
	CreateDirectoryPermissionDenied = newProblemID(
		path.Join(errorEndpointsPrefix, "create-directory-permission-denied"),
		"Permission denied while creating directories",
		http.StatusInternalServerError,
	)
	CreateDirectoryExistError = newProblemID(
		path.Join(errorEndpointsPrefix, "create-directory-exist-error"),
		"Directories already exist while creating directories",
		http.StatusInternalServerError,
	)
	CreateDirectoryInvalidPath = newProblemID(
		path.Join(errorEndpointsPrefix, "create-directory-invalid-path"),
		"Found invalid directory paths while creating directories",
		http.StatusBadRequest,
	)
	CreateDirectoryParentDirectoryExists = newProblemID(
		path.Join(errorEndpointsPrefix, "create-directory-parent-directory-exists"),
		"Parent directories already exist while creating directories",
		http.StatusInternalServerError,
	)
	CreateDirectoryParentDirectoryNoWritePermission = newProblemID(
		path.Join(errorEndpointsPrefix, "create-directory-parent-directory-no-write-permission"),
		"No write permission on parent directories while creating directories",
		http.StatusInternalServerError,
	)
	CreateDirectoryNoWritePermission = newProblemID(
		path.Join(errorEndpointsPrefix, "create-directory-no-write-permission"),
		"No write permission on directories while creating directories",
		http.StatusInternalServerError,
	)
	NonAbsolutePathError = newProblemID(
		path.Join(errorEndpointsPrefix, "non-absolute-path-error"),
		"Target path is not an absolute path",
		http.StatusBadRequest,
	)
	AuthenticationError = newProblemID(
		path.Join(errorEndpointsPrefix, "unauthorized-request"),
		"Unauthorized-request",
		http.StatusUnauthorized,
	)
	CatalogPathNotExistError = newProblemID(
		path.Join(errorEndpointsPrefix, "catalog-path-not-exist-error"),
		"Target path does not exist",
		http.StatusBadRequest,
	)
	InvalidCatalogPathError = newProblemID(
		path.Join(errorEndpointsPrefix, "invalid-catalog-path"),
		"Invalid catalog path",
		http.StatusBadRequest,
	)
	CECatalogContentDirEmptyError = newProblemID(
		path.Join(errorEndpointsPrefix, "catalog-content-dir-empty-error"),
		"Target directory is empty",
		http.StatusInternalServerError,
	)
	CECatalogContentDirNotExistError = newProblemID(
		path.Join(errorEndpointsPrefix, "catalog-content-dir-not-exist-error"),
		"Target directory does not exist",
		http.StatusInternalServerError,
	)
	GenericStartNodeError = newProblemID(
		path.Join(errorEndpointsPrefix, "start-node-command-failure-error"),
		"Start node command execution failed",
		http.StatusInternalServerError,
	)
	GenericOpenFileError = newProblemID(
		path.Join(errorEndpointsPrefix, "open-file-failure-error"),
		"Failed to open file",
		http.StatusInternalServerError,
	)
	GenericReadFileError = newProblemID(
		path.Join(errorEndpointsPrefix, "read-file-failure-error"),
		"Failed to read file",
		http.StatusInternalServerError,
	)
	GenericWriteFileError = newProblemID(
		path.Join(errorEndpointsPrefix, "write-file-failure-error"),
		"Failed to write file",
		http.StatusInternalServerError,
	)
	GenericCreateFileError = newProblemID(
		path.Join(errorEndpointsPrefix, "create-file-failure-error"),
		"Failed to create file",
		http.StatusInternalServerError,
	)
	MessageQueueFull = newProblemID(
		path.Join(errorEndpointsPrefix, "message-queue-full"),
		"Message queue is full",
		http.StatusInternalServerError,
	)
	FetchDownDatabase = newProblemID(
		path.Join(errorEndpointsPrefix, "fetch-down-database"),
		"Fetch information from a down database",
		http.StatusInternalServerError,
	)
)
