/*
 (c) Copyright [2021-2022] Open Text.
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
	// AddHosts will add IPs to the admintools.conf.  It will add the IPs to the
	// Cluster.hosts section and add a new entry (using the compat21 format) to
	// Nodes for each IP.  If a given IP is already part of admintools.conf, then
	// it will be treated as a no-op.  If the sourcePod is blank, then we will
	// create a new admintools.conf from scratch.  New admintools.conf, stored
	// in a temporary file, is returned by name.  It is the callers
	// responsibility to clean it up.
	AddHosts(ctx context.Context, sourcePod types.NamespacedName, ips []string) (string, error)

	// RemoveHosts will remove IPs from admintools.conf.  It will remove the IPs from the
	// Cluster.hosts section and any compat21 node entries.  It is expected that the
	// regular database nodes will have already been removed via 'admintools -t
	// db_remove_nodes'.  The sourcePod cannot be blank.  New admintools.conf,
	// stored in a temporary file, is returned by name to the caller.  The caller is
	// responsible for removing this file.
	RemoveHosts(ctx context.Context, sourcePod types.NamespacedName, ips []string) (string, error)
}
