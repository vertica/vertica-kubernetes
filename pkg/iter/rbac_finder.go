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

package iter

import (
	"context"
	"sort"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RBACFinder is a struct that we can use to find ServiceAccount, Roles and
// RoleBindings that are owned by the operator.
type RBACFinder struct {
	client.Client
	Vdb *vapi.VerticaDB
}

// MakeRBACFinder is a constructor for RBACFinder
func MakeRBACFinder(c client.Client, vdb *vapi.VerticaDB) RBACFinder {
	return RBACFinder{
		Client: c,
		Vdb:    vdb,
	}
}

// FindServiceAccount will find and return a Role that is owned by the operator
//
//nolint:dupl
func (r *RBACFinder) FindServiceAccount(ctx context.Context) (bool, *corev1.ServiceAccount, error) {
	sas := &corev1.ServiceAccountList{}
	if err := listObjectsOwnedByOperator(ctx, r.Client, r.Vdb, sas); err != nil {
		return false, nil, err
	}
	if len(sas.Items) == 0 {
		return false, nil, nil
	}
	sort.Slice(sas.Items, func(i, j int) bool {
		return sas.Items[i].Name < sas.Items[j].Name
	})
	return true, &sas.Items[0], nil
}

// FindRole will find and return a Role that is owned by the operator
//
//nolint:dupl
func (r *RBACFinder) FindRole(ctx context.Context) (bool, *rbacv1.Role, error) {
	roles := &rbacv1.RoleList{}
	if err := listObjectsOwnedByOperator(ctx, r.Client, r.Vdb, roles); err != nil {
		return false, nil, err
	}
	if len(roles.Items) == 0 {
		return false, nil, nil
	}
	sort.Slice(roles.Items, func(i, j int) bool {
		return roles.Items[i].Name < roles.Items[j].Name
	})
	return true, &roles.Items[0], nil
}

// FindRoleBinding will find and return a RoleBinding that is owned by the operator
//
//nolint:dupl
func (r *RBACFinder) FindRoleBinding(ctx context.Context) (bool, *rbacv1.RoleBinding, error) {
	rolebindings := &rbacv1.RoleBindingList{}
	if err := listObjectsOwnedByOperator(ctx, r.Client, r.Vdb, rolebindings); err != nil {
		return false, nil, err
	}
	if len(rolebindings.Items) == 0 {
		return false, nil, nil
	}
	sort.Slice(rolebindings.Items, func(i, j int) bool {
		return rolebindings.Items[i].Name < rolebindings.Items[j].Name
	})
	return true, &rolebindings.Items[0], nil
}
