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
	"fmt"
	"regexp"
	"strconv"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

type Components struct {
	VdbMajor, VdbMinor, VdbPatch int
}

type Info struct {
	// The version that was extracted from a Vdb
	VdbVer string
	Components
}

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
	HTTPServerMinVersion = "v12.0.1"
	// The minimum version that we can use the option with create DB to skip the
	// package install.
	CreateDBSkipPackageInstallVersion = "v12.0.1"
)

// UpgradePaths has all of the vertica releases supported by the operator.  For
// each release, the next release that must be upgrade too.  Use this map to
// know if a new version is the next supported version by Vertica.
//
// As a general rule of thumb, this map needs to be updated each time a new
// Vertica version introduces a new major or minor version (e.g. 11.1.x ->
// 12.0.x).  You don't need to update it for patch releases because we only
// enforce the upgrade path for major/minor versions.
var UpgradePaths = map[Components]Info{
	{11, 0, 0}: {"v11.1.x", Components{11, 1, 0}},
	{11, 0, 1}: {"v11.1.x", Components{11, 1, 0}},
	{11, 0, 2}: {"v11.1.x", Components{11, 1, 0}},
	{11, 1, 0}: {"v12.0.x", Components{12, 0, 0}},
	{11, 1, 1}: {"v12.0.x", Components{12, 0, 0}},
}

// MakeInfoFromVdb will construct an Info struct by extracting the version from the
// given vdb.  This returns false if it was unable to get the version from the
// vdb.
func MakeInfoFromVdb(vdb *vapi.VerticaDB) (*Info, bool) {
	vdbVer, ok := vdb.GetVerticaVersion()
	// If the version annotation isn't present, we abort creation of Info
	if !ok {
		return nil, false
	}
	return MakeInfoFromStr(vdbVer)
}

// MakeInfoFromStr will construct an Info struct by parsing the version string
func MakeInfoFromStr(ver string) (*Info, bool) {
	ma, mi, pa, ok := parseVersion(ver)
	return &Info{ver, Components{ma, mi, pa}}, ok
}

// IsUnsupported returns true if the version in the vdb is unsupported by the operator.
func (i *Info) IsUnsupported() bool {
	return !i.IsSupported()
}

// IsSupported returns true if the version in the vdb is a supported version by
// the operator.
func (i *Info) IsSupported() bool {
	return i.IsEqualOrNewer(MinimumVersion)
}

// IsEqualOrNewer returns true if the version in the Vdb is is equal or newer
// than the given version
func (i *Info) IsEqualOrNewer(inVer string) bool {
	inVerMajor, inVerMinor, inVerPatch, ok := parseVersion(inVer)
	if !ok {
		panic(fmt.Sprintf("could not parse input version: %s", inVer))
	}
	switch {
	case i.VdbMajor > inVerMajor:
		return true
	case i.VdbMajor < inVerMajor:
		return false
	case i.VdbMinor > inVerMinor:
		return true
	case i.VdbMinor < inVerMinor:
		return false
	case i.VdbPatch > inVerPatch:
		return true
	case i.VdbPatch < inVerPatch:
		return false
	}
	return true
}

// IsOlder returns true if the version in info is older than the given version
func (i *Info) IsOlder(inVer string) bool {
	return !i.IsEqualOrNewer(inVer)
}

// IsEqual compares two versions to see if they are equal
func (i *Info) IsEqual(other *Info) bool {
	return i.IsEqualExceptPatch(other) && other.VdbPatch == i.VdbPatch
}

// IsEqualExceptPatch compares two versions major/minor versions to see if they
// are equal
func (i *Info) IsEqualExceptPatch(other *Info) bool {
	return other.VdbMajor == i.VdbMajor && other.VdbMinor == i.VdbMinor
}

// IsValidUpgradePath will return true if the current version is allowed to
// upgrade to targetVer.  This will return false if the path isn't compatible.
func (i *Info) IsValidUpgradePath(targetVer string) (ok bool, failureReason string) {
	t, ok := MakeInfoFromStr(targetVer)
	if !ok {
		panic(fmt.Sprintf("could not parse input version: %s", targetVer))
	}
	// Early out for when versions are identical
	if i.IsEqual(t) {
		return true, ""
	}
	// Check for a downgrade.  Those are always blocked.
	if t.VdbMajor < i.VdbMajor ||
		(t.VdbMajor == i.VdbMajor && t.VdbMinor < i.VdbMinor) ||
		(t.VdbMajor == i.VdbMajor && t.VdbMinor == i.VdbMinor && t.VdbPatch < i.VdbPatch) {
		return false,
			fmt.Sprintf("Version '%s' to '%s' is a downgrade and is not supported",
				i.VdbVer, t.VdbVer)
	}
	// Check if the major/minor versions are identical.  It is okay to skip
	// patch versions.
	if i.IsEqualExceptPatch(t) {
		return true, ""
	}

	// Check if the upgrade path is followed.  You can only go from one released
	// version to the next released version, but patches are allowed to be skipped.
	nextVer, ok := UpgradePaths[i.Components]
	if !ok {
		// The version isn't found in the upgrade path.  This path may be
		// unsafe, but we aren't going to block this incase we are using a
		// version of vertica that came out after the version of this operator.
		return true, ""
	}
	if t.IsEqualExceptPatch(&nextVer) {
		return true, ""
	}
	return false,
		fmt.Sprintf("Version '%s' to '%s' is invalid because it skips '%s'",
			i.VdbVer, t.VdbVer, nextVer.VdbVer)
}

// parseVersion will extract out the portions of a verson into 3 components:
// major, minor and patch.
func parseVersion(v string) (major, minor, patch int, ok bool) {
	ok = false // false until known otherwise
	r := regexp.MustCompile(`v(\d+)\.(\d+)\.(\d+)`)
	m := r.FindStringSubmatch(v)
	const (
		MajorInx = iota + 1
		MinorInx
		PatchInx
		NumComponents
	)
	if len(m) != NumComponents {
		return
	}

	var err error
	major, err = strconv.Atoi(m[MajorInx])
	if err != nil {
		return
	}
	minor, err = strconv.Atoi(m[MinorInx])
	if err != nil {
		return
	}
	patch, err = strconv.Atoi(m[PatchInx])
	if err != nil {
		return
	}
	ok = true
	return
}
