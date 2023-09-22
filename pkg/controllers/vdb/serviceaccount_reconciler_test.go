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

package vdb

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("serviceaccount_reconciler", func() {
	ctx := context.Background()

	It("should create serviceaccount, rbac and rolebinding", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		r := MakeServiceAccountReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

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
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		r := MakeServiceAccountReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		finder := iter.MakeRBACFinder(k8sClient, vdb)
		exists, role, err := finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		Ω(k8sClient.Delete(ctx, role)).Should(Succeed())

		exists, _, err = finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeFalse())

		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		exists, role, err = finder.FindRole(ctx)
		Ω(err).Should(Succeed())
		Ω(exists).Should(BeTrue())
		Ω(k8sClient.Delete(ctx, role)).Should(Succeed())
	})
})
