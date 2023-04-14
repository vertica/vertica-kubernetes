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

package cloud

import "strings"

const (
	// The name of the key in the communal credential secret that holds the access key
	CommunalAccessKeyName = "accesskey"
	// The name of the key in the communal credential secret that holds the secret key
	CommunalSecretKeyName = "secretkey"
)

// IsEndpointBadError returns true if the given message text has the message aboud a bad endpoint
func IsEndpointBadError(op string) bool {
	return strings.Contains(op, "Unable to connect to endpoint")
}

// IsBucketNotExistError returns true if the given message text has the message about a bad bucket
func IsBucketNotExistError(op string) bool {
	return strings.Contains(op, "The specified bucket does not exist")
}
