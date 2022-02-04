/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package license

import (
	"context"
	"fmt"
	"sort"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPath returns the path to use for the license.  It handles the case where a
// user provided a custom license secret.
func GetPath(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB) (string, error) {
	if vdb.Spec.LicenseSecret == "" {
		return paths.CELicensePath, nil
	}

	secret := &corev1.Secret{}
	nm := types.NamespacedName{
		Namespace: vdb.Namespace,
		Name:      vdb.Spec.LicenseSecret,
	}
	if err := clnt.Get(ctx, nm, secret); err != nil {
		return "", err
	}

	if len(secret.Data) == 0 {
		return paths.CELicensePath, nil
	}

	// This function only returns a single license -- to be used with
	// admintools -t create_db.  In case the secret has multiple licenses, we will pick
	// the one that comes first alphabetically.  The rest of the licenses will
	// be mounted in the container that the customer can then install.
	licenseNames := make([]string, 0, len(secret.Data))
	for k := range secret.Data {
		licenseNames = append(licenseNames, k)
	}
	sort.Strings(licenseNames)
	return fmt.Sprintf("%s/%s", paths.MountedLicensePath, licenseNames[0]), nil
}
