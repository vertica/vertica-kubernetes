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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cache"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("passwordsecret_reconcile", func() {
	suPassword1 := "su_password1"
	suPassword2 := "su_password2"
	ctx := context.Background()

	It("should return true if spec and status have different password secret", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.PasswordSecret = suPassword1
		vdb.Status.PasswordSecret = &suPassword2
		a := PasswordSecretReconciler{Vdb: vdb, Log: logger}
		Expect(a.Vdb.IsPasswordSecretChanged(vapi.MainCluster)).To(BeTrue())
	})

	It("should return false if spec and status have the same password secret", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.PasswordSecret = suPassword1
		vdb.Status.PasswordSecret = &suPassword1
		a := PasswordSecretReconciler{Vdb: vdb, Log: logger}
		Expect(a.Vdb.IsPasswordSecretChanged(vapi.MainCluster)).To(BeFalse())
	})

	It("should update status when status.passwordSecret is nil", func() {
		vdb := vapi.MakeVDB()

		secret1 := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      suPassword1,
				Namespace: vdb.Namespace,
			},
			Data: map[string][]byte{
				names.SuperuserPasswordKey: []byte(suPassword1),
			},
		}

		vdb.Spec.PasswordSecret = secret1.Name
		vdb.Status.PasswordSecret = nil // status is nil before vdb was initialized

		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pFacts := podfacts.MakePodFacts(vdbRec, fpr, logger, &secret1.Name)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, &secret1.Name)
		prunner := &cmds.FakePodRunner{}
		cache := cache.MakeCacheManager(true)
		rec := MakePasswordSecretReconciler(vdbRec, logger, vdb, prunner, &pFacts, dispatcher, cache, nil)
		r := rec.(*PasswordSecretReconciler)
		Expect(r.updatePasswordSecretStatus(ctx)).Should(BeNil())

		// Verify the status was updated to match spec
		Expect(vdb.Status.PasswordSecret).ShouldNot(BeNil())
		Expect(*vdb.Status.PasswordSecret).Should(Equal(vdb.Spec.PasswordSecret))
	})
})
