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

package atconf

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
)

type Writer interface {
	// AddHosts will add a list of IPs to the admintools.conf.  Caller provides
	// the pod that has the admintools.conf that we will build upon.  If the
	// sourcePod is blank, then we will create a new admintools.conf from
	// scratch.  The contents of the new admintools.conf is stored in a
	// temporary file that we return by name.  The caller is responsible for
	// removing this temp file.
	AddHosts(ctx context.Context, sourcePod types.NamespacedName, ips []string) (string, error)
}
