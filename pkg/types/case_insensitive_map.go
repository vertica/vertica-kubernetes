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

package types

import (
	"strings"
)

// CiMap is a thin wrapper around a map to allow
// case insensitive keys.
type CiMap struct {
	m map[string]string
}

// MakeCiMap is a helper that constructs a CiMap struct.
func MakeCiMap() *CiMap {
	return &CiMap{m: make(map[string]string)}
}

// Set inserts a key and its value into the map.
func (c *CiMap) Set(k, v string) {
	c.m[strings.ToLower(k)] = v
}

// Get returns two values: the actual value for the
// key and a boolean that indicates if the key exists.
func (c *CiMap) Get(k string) (string, bool) {
	v, ok := c.m[strings.ToLower(k)]
	return v, ok
}

// GetMap extracts the inner map from the wrapper.
func (c *CiMap) GetMap() map[string]string {
	return c.m
}

// ContainKeyValuePair returns true if the specified key
// exists and has as value the specified value.
func (c *CiMap) ContainKeyValuePair(key, val string) bool {
	v, ok := c.Get(key)
	if !ok {
		return false
	}
	return v == val
}

// GetValue returns the value for the given key and an empty
// string if the key does not exist.
func (c *CiMap) GetValue(k string) string {
	v, ok := c.Get(k)
	if !ok {
		return ""
	}
	return v
}

// Size returns the number of elements in the map.
func (c *CiMap) Size() int {
	return len(c.m)
}
