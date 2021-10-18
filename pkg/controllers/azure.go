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
)

// AzureCredential stores the credentials to connect to azb://
type AzureCredential struct {
	// At least one of the next two need to be set
	accountName  string
	blobEndpoint string // host name with optional port (host:port)

	// Only one of the two will be set
	accountKey            string // Access key for the account or endpoint
	sharedAccessSignature string // Access token for finer-grained access control
}
