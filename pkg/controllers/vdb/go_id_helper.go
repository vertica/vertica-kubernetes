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

// Remove this file after VER-89903 is closed since the function
// goid() inside this file is slow and not recommended by Go.
package vdb

import (
	"bytes"
	"errors"
	"runtime"
	"strconv"
)

const bufferSize = 32

var (
	goroutinePrefix = []byte("goroutine ")
	errBadStack     = errors.New("invalid runtime.Stack output")
)

func goid() (int, error) {
	buf := make([]byte, bufferSize)
	n := runtime.Stack(buf, false)
	buf = buf[:n]

	buf, ok := bytes.CutPrefix(buf, goroutinePrefix)
	if !ok {
		return 0, errBadStack
	}

	i := bytes.IndexByte(buf, ' ')
	if i < 0 {
		return 0, errBadStack
	}

	return strconv.Atoi(string(buf[:i]))
}
