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

package vdb

import (
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
)

// hasCompatibleVersionForHTTPServer checks, in case http server is enabled, if
// the server has a required version for http server.
func hasCompatibleVersionForHTTPServer(vrec *VerticaDBReconciler, vdb *vapi.VerticaDB, logEvent bool, action string) bool {
	// Early out if the http service isn't enabled
	if !vdb.IsHTTPServerEnabled() {
		return false
	}
	vinf, ok := vdb.MakeVersionInfo()
	if !ok || vinf.IsOlder(vapi.HTTPServerMinVersion) {
		if logEvent {
			eventMsg := "Skipping %s because the Vertica version doesn't have " +
				"support for it. A Vertica version of '%s' or newer is needed"
			vrec.Eventf(vdb, corev1.EventTypeWarning, events.HTTPServerNotSetup, eventMsg, action, vapi.HTTPServerMinVersion)
		}
		return false
	}
	return true
}
