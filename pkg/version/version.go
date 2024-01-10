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

// MakeInfoFromStr will construct an Info struct by parsing the version string
func MakeInfoFromStr(curVer string) (*Info, bool) {
	ma, mi, pa, ok := parseVersion(curVer)
	return &Info{curVer, Components{ma, mi, pa}}, ok
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
