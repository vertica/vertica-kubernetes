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

package version

import (
	"fmt"
	"regexp"
	"strconv"
)

type Components struct {
	VdbMajor, VdbMinor, VdbPatch int
}

type Info struct {
	VdbVer     string // The version that was extracted from a vdb
	Components        // The same version as VdbVer but broken down into individual components
}

type ComparisonResult int

const (
	CompareEqual   ComparisonResult = iota // CompareEqual represents the state where versions are equal
	CompareLarger                          // CompareLarger represents the state where the first version is larger
	CompareSmaller                         // CompareSmaller represents the state where the first version is smaller
)

const (
	LTSMinor = 4
	LTSPatch = 0
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
	{12, 0, 0}: {"v23.3.x", Components{23, 4, 0}},
	{12, 0, 1}: {"v23.3.x", Components{23, 4, 0}},
	{12, 0, 2}: {"v23.3.x", Components{23, 4, 0}},
	{12, 0, 3}: {"v23.3.x", Components{23, 4, 0}},
	{12, 0, 4}: {"v23.3.x", Components{23, 4, 0}},
}

// MakeInfoFromStr will construct an Info struct by parsing the version string
func MakeInfoFromStr(curVer string) (*Info, bool) {
	ma, mi, pa, ok := parseVersion(curVer)
	return &Info{curVer, Components{ma, mi, pa}}, ok
}

// buildVersionStr will build the version string from
// a Components object
func (c *Components) buildVersionStr() string {
	return fmt.Sprintf("v%d.%d.%d", c.VdbMajor, c.VdbMinor, c.VdbPatch)
}

func (i *Info) compareVersion(major, minor, patch int) ComparisonResult {
	switch {
	case i.VdbMajor > major:
		return CompareLarger
	case i.VdbMajor < major:
		return CompareSmaller
	case i.VdbMinor > minor:
		return CompareLarger
	case i.VdbMinor < minor:
		return CompareSmaller
	case i.VdbPatch > patch:
		return CompareLarger
	case i.VdbPatch < patch:
		return CompareSmaller
	}
	return CompareEqual
}

// IsEqualOrNewer returns true if the version in the Vdb is equal or newer
// than the given version
func (i *Info) IsEqualOrNewer(inVer string) bool {
	inVerMajor, inVerMinor, inVerPatch, ok := parseVersion(inVer)
	if !ok {
		panic(fmt.Sprintf("could not parse input version: %s", inVer))
	}
	res := i.compareVersion(inVerMajor, inVerMinor, inVerPatch)
	return res != CompareSmaller
}

// IsOlder returns true if the version in info is older than the given version
func (i *Info) IsOlder(inVer string) bool {
	return !i.IsEqualOrNewer(inVer)
}

// IsEqual compares two versions to see if they are equal
func (i *Info) IsEqual(other *Info) bool {
	return i.IsEqualExceptPatch(other) && other.VdbPatch == i.VdbPatch
}

// IsUnsupported returns true if the version in the vdb is unsupported by the operator.
func (i *Info) IsUnsupported(minVersion string) bool {
	return !i.IsSupported(minVersion)
}

// IsSupported returns true if the version in the vdb is a supported version by
// the operator.
func (i *Info) IsSupported(minVersion string) bool {
	return i.IsEqualOrNewer(minVersion)
}

// IsEqualExceptPatch compares two versions major/minor versions to see if they
// are equal
func (i *Info) IsEqualExceptPatch(other *Info) bool {
	return other.VdbMajor == i.VdbMajor && other.VdbMinor == i.VdbMinor
}

// IsOlderExceptPatch returns true if the receiver's major/minor version
// is lower than the given version.
func (i *Info) IsOlderExceptPatch(other *Info) bool {
	return i.VdbMajor < other.VdbMajor ||
		(i.VdbMajor == other.VdbMajor && i.VdbMinor < other.VdbMinor)
}

// IsOlderOrEqualExceptPatch compares two versions major/minor versions to see if the
// first one is equal or older than the second
func (i *Info) IsOlderOrEqualExceptPatch(other *Info) bool {
	return i.IsEqualExceptPatch(other) || i.IsOlderExceptPatch(other)
}

// isLTSRelease returns true if the release is LTS
// meaning its version has the format x.4.x
func (i *Info) isLTSRelease() bool {
	return i.VdbMinor == LTSMinor
}

// getNextLTSVersion given a release returns the next
// LTS realease version
func (i *Info) getNextLTSVersion() Info {
	nextLTSVersion := Info{
		VdbVer: i.VdbVer,
	}
	nextLTSVersion.VdbMajor = i.VdbMajor
	nextLTSVersion.VdbMinor = LTSMinor
	nextLTSVersion.VdbPatch = LTSPatch
	if i.isLTSRelease() {
		nextLTSVersion.VdbMajor++
		nextLTSVersion.VdbVer = nextLTSVersion.buildVersionStr()
	}
	return nextLTSVersion
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
		// The version isn't found in the upgrade path. It must be a version
		// newer that v12.0.4. Since from that point our release versions are
		// more predictable, we can get the max version the current version
		// can be upgraded to from the next LTS release version,
		nextVer = i.getNextLTSVersion()
	}
	if t.IsOlderOrEqualExceptPatch(&nextVer) {
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
