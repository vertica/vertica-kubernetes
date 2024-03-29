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

package vdb

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("serviceaccount_reconciler", func() {
	ctx := context.Background()

	It("should create serviceaccount, rbac and rolebinding", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		r := MakeServiceAccountReconciler(vdbRec, logger, vdb)
		Ω(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		finder := iter.MakeRBACFinder(k8sClient, vdb)
		exists, sa, err := finder.FindServiceAccount(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		defer func() { _ = k8sClient.Delete(ctx, sa) }()

		var role *rbacv1.Role
		exists, role, err = finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		defer func() { _ = k8sClient.Delete(ctx, role) }()

		var roleBinding *rbacv1.RoleBinding
		exists, roleBinding, err = finder.FindRoleBinding(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		defer func() { _ = k8sClient.Delete(ctx, roleBinding) }()

		Ω(roleBinding.RoleRef.Name).Should(Equal(role.Name))
		Ω(roleBinding.Subjects).Should(HaveLen(1))
		Ω(roleBinding.Subjects[0].Name).Should(Equal(sa.Name))
	})

	It("should create recreate role if missing", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		r := MakeServiceAccountReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		finder := iter.MakeRBACFinder(k8sClient, vdb)
		exists, sa, err := finder.FindServiceAccount(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		defer func() { _ = k8sClient.Delete(ctx, sa) }()

		exists, roleBinding, err := finder.FindRoleBinding(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		defer func() { _ = k8sClient.Delete(ctx, roleBinding) }()

		exists, role, err := finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		Ω(k8sClient.Delete(ctx, role)).Should(Succeed())

		exists, _, err = finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeFalse())

		Ω(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		exists, role, err = finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		Ω(k8sClient.Delete(ctx, role)).Should(Succeed())
	})

	It("should skip role/rolebinding creation if user provided serviceAccount", func() {
		vdb := vapi.MakeVDB()
		const saName = "ut-sa"
		userSa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      saName,
				Namespace: vdb.Namespace,
			},
		}
		Ω(k8sClient.Create(ctx, userSa))
		defer func() { Ω(k8sClient.Delete(ctx, userSa)) }()
		vdb.Spec.ServiceAccountName = saName
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeServiceAccountReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		finder := iter.MakeRBACFinder(k8sClient, vdb)
		exists, _, err := finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeFalse())

		exists, _, err = finder.FindRoleBinding(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeFalse())

		exists, _, err = finder.FindServiceAccount(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeFalse())
	})

	It("should recreate sa/role/rolebinding if sa name is set but doesn't exist", func() {
		vdb := vapi.MakeVDB()
		const saName = "ut-sa" // Intentionally don't create this
		vdb.Spec.ServiceAccountName = saName
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeServiceAccountReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		finder := iter.MakeRBACFinder(k8sClient, vdb)
		exists, role, err := finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		defer func() { _ = k8sClient.Delete(ctx, role) }()

		exists, roleBinding, err := finder.FindRoleBinding(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		defer func() { _ = k8sClient.Delete(ctx, roleBinding) }()

		exists, sa, err := finder.FindServiceAccount(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		defer func() { _ = k8sClient.Delete(ctx, sa) }()
		Ω(sa.Name).Should(Equal(saName))
	})
})
