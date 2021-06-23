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

package version

import (
	"fmt"
	"regexp"
	"strconv"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

type Info struct {
	// The version that was extracted from a Vdb
	VdbVer                       string
	VdbMajor, VdbMinor, VdbPatch int
}

const (
	// This is the minimum vertica version that the operator currently supports.
	// If the vertica image that we deploy is older than this then the operator
	// abort the reconiliation process.
	MinimumVersion = "v10.1.1"
)

// MakeInfo will construct an Info struct by extracting the version from the
// given vdb.  This returns false if it was unable to get the version from the
// vdb.
func MakeInfo(vdb *vapi.VerticaDB) (*Info, bool) {
	vdbVer, ok := vdb.GetVerticaVersion()
	// If the version annotation isn't present, we abort creation of Info
	if !ok {
		return nil, false
	}
	ma, mi, pa, ok := parseVersion(vdbVer)
	return &Info{vdbVer, ma, mi, pa}, ok
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
