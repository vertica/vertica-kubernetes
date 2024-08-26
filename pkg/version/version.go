/*
 (c) Copyright [2021-2024] Open Text.
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
	VdbMajor, VdbMinor, VdbPatch, VdbHotfix int
}

type Info struct {
	VdbVer     string // The version that was extracted from a vdb
	Components        // The same version as VdbVer but broken down into individual components
}

type ComparisonResult int

const (
	compareEqual   ComparisonResult = iota // CompareEqual represents the state where versions are equal
	compareLarger                          // CompareLarger represents the state where the first version is larger
	compareSmaller                         // CompareSmaller represents the state where the first version is smaller
)

// MakeInfoFromStr will construct an Info struct by parsing the version string
func MakeInfoFromStr(curVer string) (*Info, bool) {
	comp, ok := parseVersion(curVer)
	return &Info{curVer, comp}, ok
}

func (i *Info) compareVersion(comp Components) ComparisonResult {
	switch {
	case i.VdbMajor > comp.VdbMajor:
		return compareLarger
	case i.VdbMajor < comp.VdbMajor:
		return compareSmaller
	case i.VdbMinor > comp.VdbMinor:
		return compareLarger
	case i.VdbMinor < comp.VdbMinor:
		return compareSmaller
	case i.VdbPatch > comp.VdbPatch:
		return compareLarger
	case i.VdbPatch < comp.VdbPatch:
		return compareSmaller
	}
	return compareEqual
}

// MakeInfoFromStrCheck is like MakeInfoFromStr but returns an error
// if the version Info cannot be constructed from the version string
func MakeInfoFromStrCheck(curVer string) (*Info, error) {
	verInfo, ok := MakeInfoFromStr(curVer)
	if !ok {
		return nil, fmt.Errorf("could not construct Info struct from the version string %s", curVer)
	}
	return verInfo, nil
}

// IsEqualOrNewer returns true if the version in the Vdb is is equal or newer
// than the given version
func (i *Info) IsEqualOrNewer(inVer string) bool {
	comp, ok := parseVersion(inVer)
	if !ok {
		panic(fmt.Sprintf("could not parse input version: %s", inVer))
	}
	res := i.compareVersion(comp)
	return res != compareSmaller
}

// HasEqualOrNewerHotfix checks if both versions have the same major, minor, patch numbers, and
// if hotfix number in source version is equal or newer than the one in target version
func (i *Info) HasEqualOrNewerHotfix(inVer string) bool {
	comp, ok := parseVersion(inVer)
	if !ok {
		panic(fmt.Sprintf("could not parse input version: %s", inVer))
	}
	res := i.compareVersion(comp)
	return res == compareEqual && i.VdbHotfix >= comp.VdbHotfix
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
	// Check for a downgrade.
	if t.VdbMajor < i.VdbMajor ||
		(t.VdbMajor == i.VdbMajor && t.VdbMinor < i.VdbMinor) ||
		(t.VdbMajor == i.VdbMajor && t.VdbMinor == i.VdbMinor && t.VdbPatch < i.VdbPatch) {
		return false,
			fmt.Sprintf("Version '%s' to '%s' is a downgrade and is not supported",
				i.VdbVer, t.VdbVer)
	}
	return true, ""
}

// parseVersion will extract out the portions of a verson into 4 components:
// major, minor, patch and hotfix.
func parseVersion(v string) (Components, bool) {
	comp := Components{}
	r := regexp.MustCompile(`v(\d+)\.(\d+)\.(\d+)(?:-(\d+))?`)
	m := r.FindStringSubmatch(v)
	const (
		MajorInx = iota + 1
		MinorInx
		PatchInx
		HotfixInx
		NumComponents
	)
	if len(m) < NumComponents {
		return comp, false
	}

	var err error
	comp.VdbMajor, err = strconv.Atoi(m[MajorInx])
	if err != nil {
		return comp, false
	}
	comp.VdbMinor, err = strconv.Atoi(m[MinorInx])
	if err != nil {
		return comp, false
	}
	comp.VdbPatch, err = strconv.Atoi(m[PatchInx])
	if err != nil {
		return comp, false
	}
	if m[HotfixInx] != "" {
		comp.VdbHotfix, err = strconv.Atoi(m[HotfixInx])
		if err != nil {
			return comp, false
		}
	}
	return comp, true
}
