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

package version

import (
	"regexp"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

// MergeAnnotations will merge new annotations with vdb.  It will return true if
// any annotation changed.  Caller is responsible for updating the Vdb in the
// API server.
func MergeAnnotations(vdb *vapi.VerticaDB, newAnnotations map[string]string) bool {
	changedAnnotations := false
	for k, newValue := range newAnnotations {
		oldValue, ok := vdb.ObjectMeta.Annotations[k]
		if !ok || oldValue != newValue {
			if vdb.ObjectMeta.Annotations == nil {
				vdb.ObjectMeta.Annotations = map[string]string{}
			}
			vdb.ObjectMeta.Annotations[k] = newValue
			changedAnnotations = true
		}
	}
	return changedAnnotations
}

// ParseVersionOutput will parse the raw output from the --version call and
// build an annotation map.
// nolint:lll
func ParseVersionOutput(op string) map[string]string {
	// Sample output looks like this:
	// Vertica Analytic Database v11.0.0-20210601
	// vertica(v11.0.0-20210601) built by @re-docker2 from master@da8f0e93f1ee720d8e4f8e1366a26c0d9dd7f9e7 on 'Tue Jun  1 05:04:35 2021' $BuildId$
	regMap := map[string]string{
		vapi.VersionAnnotation:   `(v[0-9a-zA-Z.-]+)\n`,
		vapi.BuildRefAnnotation:  `built by .* from .*@([^ ]+) `,
		vapi.BuildDateAnnotation: `on '([A-Za-z0-9: ]+)'`,
	}

	// We build up this annotation map while we iterate through each regular
	// expression
	annotations := map[string]string{}

	for annName, reStr := range regMap {
		r := regexp.MustCompile(reStr)
		parse := r.FindStringSubmatch(op)
		const MinStringMatch = 2 // [0] is for the entire string, [1] is for the submatch
		if len(parse) >= MinStringMatch {
			annotations[annName] = parse[1]
		}
	}
	return annotations
}

// IsUpgradePathSupported returns true if the version annotations is a valid
// version transition from the version in the Vdb.
func IsUpgradePathSupported(vdb *vapi.VerticaDB, newAnnotations map[string]string) (ok bool, failureReason string) {
	vinf, makeOk := MakeInfoFromVdb(vdb)
	if !makeOk {
		// Version info is not in the vdb.  Fine to skip.
		return true, ""
	}
	ok, failureReason = vinf.IsValidUpgradePath(newAnnotations[vapi.VersionAnnotation])
	return
}
