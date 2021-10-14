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

import "strings"

const (
	// The name of the key in the communal credential secret that holds the access key
	CommunalAccessKeyName = "accesskey"
	// The name of the key in the communal credential secret that holds the secret key
	CommunalSecretKeyName = "secretkey"
)

// isEndpointBadError returns true if the given message text has the message aboud a bad endpoint
func isEndpointBadError(op string) bool {
	return strings.Contains(op, "Unable to connect to endpoint")
}

// isBucketNotExistError returns true if the given message text has the message about a bad bucket
func isBucketNotExistError(op string) bool {
	return strings.Contains(op, "The specified bucket does not exist")
}
