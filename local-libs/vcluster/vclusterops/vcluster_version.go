/*
 (c) Copyright [2023-2024] Open Text.
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

package vclusterops

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type semVer struct {
	Ver   string `json:"ver"`
	Major string `json:"-"`
	Minor string `json:"-"`
	Patch string `json:"-"`
}

type VclusterOpVersion struct {
	Origin string `json:"origin"`
	SemVer semVer
}

func (semVersion *semVer) parseComponentsIfNecessary() error {
	cleanSize := strings.TrimSpace(semVersion.Ver)
	r := regexp.MustCompile(`^(\d+)\.(\d+).(\d+)$`)
	matches := r.FindAllStringSubmatch(cleanSize, -1)
	if len(matches) != 1 {
		return fmt.Errorf("parse error for version %s: It is not a valid version", semVersion.Ver)
	}
	semVersion.Major = matches[0][1]
	semVersion.Minor = matches[0][2]
	semVersion.Patch = matches[0][3]
	return nil
}

func (semVersion *semVer) incompatibleVersion(otherVer *semVer) (bool, error) {
	err := semVersion.parseComponentsIfNecessary()
	if err != nil {
		return false, err
	}
	majorStr := semVersion.Major
	err = otherVer.parseComponentsIfNecessary()
	if err != nil {
		return false, err
	}
	majorOtherVerStr := otherVer.Major
	return majorStr == majorOtherVerStr, nil
}

func (semVersion *semVer) equalVersion(otherVer *semVer) bool {
	return otherVer.Ver == semVersion.Ver
}

func (opVersion *VclusterOpVersion) equalVclusterVersion(otherVer *VclusterOpVersion) bool {
	return opVersion.Origin == otherVer.Origin && opVersion.SemVer.equalVersion(&otherVer.SemVer)
}

func (opVersion *VclusterOpVersion) convertVclusterVersionToJSON() (string, error) {
	SemVer := &semVer{Ver: opVersion.SemVer.Ver}
	vclusterVersionData := map[string]any{
		"origin": opVersion.Origin,
		"semver": SemVer,
	}
	jsonFile, err := json.Marshal(vclusterVersionData)
	if err != nil {
		return "", fmt.Errorf("could not marshal json: %w", err)
	}
	return string(jsonFile), nil
}

func vclusterVersionFromDict(vclusterVersionDict map[string]string) (VclusterOpVersion, error) {
	requiredKeys := []string{"origin", "semver"}
	for _, key := range requiredKeys {
		if _, ok := vclusterVersionDict[key]; !ok {
			return VclusterOpVersion{}, fmt.Errorf("%s is missing one or more required fields", vclusterVersionDict)
		}
	}
	return VclusterOpVersion{Origin: vclusterVersionDict["origin"], SemVer: semVer{Ver: vclusterVersionDict["semver"]}}, nil
}
