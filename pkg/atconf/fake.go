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

package atconf

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
)

// FakeWriter is a fake admintools.conf writer for testing purposes
type FakeWriter struct {
}

// AddHosts is called to add IPs to an admintools.conf.  New admintools.conf, stored in
// a temporarily, is returned by name.  Since this is fake, we just return a
// name of a dummy file.  Actual file doesn't exist.
func (f *FakeWriter) AddHosts(_ context.Context, _ types.NamespacedName, _ []string) (string, error) {
	return "admintools.conf.tmp", nil
}

// RemoveHosts is called to remove hosts from admintools.conf.
func (f *FakeWriter) RemoveHosts(ctx context.Context, sourcePod types.NamespacedName, ips []string) (string, error) {
	// no-op, do the same thing as AddHosts
	return f.AddHosts(ctx, sourcePod, ips)
}
