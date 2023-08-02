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

package v1beta1

import (
	"regexp"

	"github.com/vertica/vertica-kubernetes/pkg/version"
)

const (
	// This is the minimum vertica version that the operator currently supports.
	// If the vertica image that we deploy is older than this then the operator
	// aborts the reconiliation process.
	MinimumVersion = "v11.0.1"
	// The version that added read-only state
	NodesHaveReadOnlyStateVersion = "v11.0.2"
	// The minimum version that allows for online upgrade.
	OnlineUpgradeVersion = "v11.1.0"
	// The version that added the --force option to reip to handle up nodes
	ReIPAllowedWithUpNodesVersion = "v11.1.0"
	// The version of the server that doesn't support cgroup v2
	CGroupV2UnsupportedVersion = "v12.0.0"
	// The minimum version that can start Vertica's http server
	HTTPServerMinVersion = "v12.0.3"
	// When httpServerMode is Auto, this is the minimum server version that will start Vertica's http server
	HTTPServerAutoMinVersion = "v12.0.4"
	// The minimum version that we can use the option with create DB to skip the
	// package install.
	CreateDBSkipPackageInstallVersion = "v12.0.1"
	// Starting in v23.4.0, we added some new config parameters for settings
	// that were typically done post create using SQL -- setting the default
	// subcluster name and preferred k-safety.
	DBSetupConfigParametersMinVersion = "v23.4.0"
	// In 23.3.0, the EncryptSpreadComm config parm can be set during the create
	// db call. On versions prior to this, it must be specified immediately
	// after via SQL.
	SetEncryptSpreadCommAsConfigVersion = "v23.3.0"
)

// GetVerticaVersionStr returns the vertica version, in string form, that is stored
// within the vdb
func (v *VerticaDB) GetVerticaVersionStr() (string, bool) {
	ver, ok := v.ObjectMeta.Annotations[VersionAnnotation]
	return ver, ok
}

// MakeVersionInfo will construct an Info struct by extracting the version from the
// given vdb.  This returns false if it was unable to get the version from the
// vdb.
func (v *VerticaDB) MakeVersionInfo() (*version.Info, bool) {
	vdbVer, ok := v.GetVerticaVersionStr()
	// If the version annotation isn't present, we abort creation of Info
	if !ok {
		return nil, false
	}
	return version.MakeInfoFromStr(vdbVer)
}

// ParseVersionOutput will parse the raw output from the --version call and
// build an annotation map.
//
//nolint:lll
func ParseVersionOutput(op string) map[string]string {
	// Sample output looks like this:
	// Vertica Analytic Database v11.0.0-20210601
	// vertica(v11.0.0-20210601) built by @re-docker2 from master@da8f0e93f1ee720d8e4f8e1366a26c0d9dd7f9e7 on 'Tue Jun  1 05:04:35 2021' $BuildId$
	regMap := map[string]string{
		VersionAnnotation:   `(v[0-9a-zA-Z.-]+)\n`,
		BuildRefAnnotation:  `built by .* from .*@([^ ]+) `,
		BuildDateAnnotation: `on '([A-Za-z0-9: ]+)'`,
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
func (v *VerticaDB) IsUpgradePathSupported(newAnnotations map[string]string) (ok bool, failureReason string) {
	vinf, makeOk := v.MakeVersionInfo()
	if !makeOk {
		// Version info is not in the vdb.  Fine to skip.
		return true, ""
	}
	ok, failureReason = vinf.IsValidUpgradePath(newAnnotations[VersionAnnotation])
	return
}
