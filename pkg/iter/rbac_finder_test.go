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

//nolint:dupl
package iter

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("rbac_finder", func() {
	ctx := context.Background()

	It("should find created role", func() {
		vdb := vapi.MakeVDB()
		finder := MakeRBACFinder(k8sClient, vdb)

		exists, _, err := finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeFalse())

		createdRole, err := createFakeRole(ctx, k8sClient, vdb)
		Ω(err).Should(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, createdRole) }()

		var foundRole *rbacv1.Role
		exists, foundRole, err = finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		Ω(foundRole.Name).Should(Equal(createdRole.Name))
	})

	It("should find created serviceAccount", func() {
		vdb := vapi.MakeVDB()
		finder := MakeRBACFinder(k8sClient, vdb)

		exists, _, err := finder.FindServiceAccount(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeFalse())

		createdSA, err := createFakeServiceAccount(ctx, k8sClient, vdb)
		Ω(err).Should(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, createdSA) }()

		var foundSA *corev1.ServiceAccount
		exists, foundSA, err = finder.FindServiceAccount(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		Ω(foundSA.Name).Should(Equal(createdSA.Name))
	})

	It("should find created roleBinding", func() {
		vdb := vapi.MakeVDB()
		finder := MakeRBACFinder(k8sClient, vdb)

		exists, _, err := finder.FindRoleBinding(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeFalse())

		createdSA, err := createFakeServiceAccount(ctx, k8sClient, vdb)
		Ω(err).Should(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, createdSA) }()

		createdRole, err := createFakeRole(ctx, k8sClient, vdb)
		Ω(err).Should(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, createdRole) }()

		createdRB, err := createFakeRoleBinding(ctx, k8sClient, vdb, createdSA, createdRole)
		Ω(err).Should(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, createdRB) }()

		var foundRB *rbacv1.RoleBinding
		exists, foundRB, err = finder.FindRoleBinding(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		Ω(foundRB.Name).Should(Equal(createdRB.Name))
	})
})

// createFakeServiceAccount will create a new service account for test purposes
func createFakeServiceAccount(ctx context.Context, cl client.Client, vdb *vapi.VerticaDB) (*corev1.ServiceAccount, error) {
	sa := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sa",
			Namespace: "default",
			Labels:    builder.MakeCommonLabels(vdb, nil, false, false),
		},
	}
	err := cl.Create(ctx, &sa)
	return &sa, err
}

// createFakeRole will create a Role suitable for test purposes
func createFakeRole(ctx context.Context, cl client.Client, vdb *vapi.VerticaDB) (*rbacv1.Role, error) {
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-role",
			Namespace: "default",
			Labels:    builder.MakeCommonLabels(vdb, nil, false, false),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs: []string{
					"get",
					"list",
				},
			},
		},
	}
	err := cl.Create(ctx, &role)
	return &role, err
}

// createFakeRoleBinding will create a new RoleBinding to connects the given
// ServiceAccount with the given Role
func createFakeRoleBinding(ctx context.Context, cl client.Client, vdb *vapi.VerticaDB,
	sa *corev1.ServiceAccount, role *rbacv1.Role) (*rbacv1.RoleBinding, error) {
	rolebinding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rb",
			Namespace: "default",
			Labels:    builder.MakeCommonLabels(vdb, nil, false, false),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     role.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      sa.Name,
				Namespace: sa.Namespace,
			},
		},
	}
	err := cl.Create(ctx, &rolebinding)
	return &rolebinding, err
}
