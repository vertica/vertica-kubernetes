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

package controllers

const (
	// Key names in the communal credentials for Azure blob storage endpoints.
	AzureAccountName           = "accountName"
	AzureBlobEndpoint          = "blobEndpoint"
	AzureAccountKey            = "accountKey"
	AzureSharedAccessSignature = "sharedAccessSignature"
	AzureDefaultProtocol       = "HTTPS"
)

// AzureCredential stores the credentials to connect to azb://
// This structure must be kept in sync with the Vertica server format.  You
// cannot add fields here unless there is a corresponding change in the engine.
type AzureCredential struct {
	// At least one of the next two need to be set
	AccountName  string `json:"accountName,omitempty"`
	BlobEndpoint string `json:"blobEndpoint,omitempty"` // host name with optional port (host:port)

	// Only one of the two will be set
	AccountKey            string `json:"accountKey,omitempty"`            // Access key for the account or endpoint
	SharedAccessSignature string `json:"sharedAccessSignature,omitempty"` // Access token for finer-grained access control
}

// AzureEndpointConfig contains config elements for a single azure endpoint.
// This structure must be kept insync with the Vertica server format.
type AzureEndpointConfig struct {
	AccountName            string `json:"accountName,omitempty"`
	BlobEndpoint           string `json:"blobEndpoint,omitempty"`
	Protocol               string `json:"protocol,omitempty"`
	IsMultiAccountEndpoint bool   `json:"isMultiAccountEndpoint,omitempty"`
}
