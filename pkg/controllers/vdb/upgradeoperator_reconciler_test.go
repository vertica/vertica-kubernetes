/*
 (c) Copyright [2021-2025] Open Text.
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
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("k8s/upgradeoperator_reconciler", func() {
	ctx := context.Background()
	const trueStr = "true"

	It("should delete sts that was created prior to current release", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		nm := names.GenStsName(vdb, sc)
		sts := builder.BuildStsSpec(nm, vdb, sc, "")
		// Set an old operator version to force the upgrade
		sts.Labels[vmeta.OperatorVersionLabel] = "2.1.2"
		Expect(k8sClient.Create(ctx, sts)).Should(Succeed())
		defer func() {
			delSts := &appsv1.StatefulSet{}
			err := k8sClient.Get(ctx, nm, delSts)
			if !errors.IsNotFound(err) {
				Expect(k8sClient.Delete(ctx, sts)).Should(Succeed())
			}
		}()

		fetchedSts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, nm, fetchedSts)).Should(Succeed())

		r := MakeUpgradeOperatorReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		// Reconcile should have deleted the sts because it was created by an
		// old operator version
		Expect(k8sClient.Get(ctx, nm, fetchedSts)).ShouldNot(Succeed())
	})

	It("should set TLS enabled fields if operator version is older than 25.4.0", func() {
		vdb := vapi.MakeVDB()
		// Set up HTTPSNMATLS and ClientServerTLS with nil Enabled fields
		vdb.Spec.HTTPSNMATLS = &vapi.TLSConfigSpec{}
		vdb.Spec.ClientServerTLS = &vapi.TLSConfigSpec{}
		// Add annotation to simulate TLS auth enabled
		if vdb.Annotations == nil {
			vdb.Annotations = map[string]string{}
		}
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueStr
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		sc := &vdb.Spec.Subclusters[0]
		nm := names.GenStsName(vdb, sc)
		sts := builder.BuildStsSpec(nm, vdb, sc, "")
		sts.Labels[vmeta.OperatorVersionLabel] = "25.3.0" // older than 25.4.0

		Expect(k8sClient.Create(ctx, sts)).Should(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, sts)
		}()

		r := MakeUpgradeOperatorReconciler(vdbRec, logger, vdb)
		Expect(r.(*UpgradeOperatorReconciler).SetTLSEnabled(ctx, []appsv1.StatefulSet{*sts})).Should(Succeed())

		// Fetch the updated vdb to check the fields
		updatedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), updatedVdb)).Should(Succeed())
		Expect(updatedVdb.Spec.HTTPSNMATLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.HTTPSNMATLS.Enabled).ShouldNot(BeNil())
		Expect(*updatedVdb.Spec.HTTPSNMATLS.Enabled).Should(BeTrue())
		Expect(updatedVdb.Spec.ClientServerTLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.ClientServerTLS.Enabled).ShouldNot(BeNil())
		Expect(*updatedVdb.Spec.ClientServerTLS.Enabled).Should(BeTrue())
	})

	It("should not set TLS enabled fields if operator version is 25.4.0 or newer", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPSNMATLS = &vapi.TLSConfigSpec{}
		vdb.Spec.ClientServerTLS = &vapi.TLSConfigSpec{}
		if vdb.Annotations == nil {
			vdb.Annotations = map[string]string{}
		}
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueStr
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		sc := &vdb.Spec.Subclusters[0]
		nm := names.GenStsName(vdb, sc)
		sts := builder.BuildStsSpec(nm, vdb, sc, "")
		sts.Labels[vmeta.OperatorVersionLabel] = vmeta.OperatorVersion254 // not older

		Expect(k8sClient.Create(ctx, sts)).Should(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, sts)
		}()

		r := MakeUpgradeOperatorReconciler(vdbRec, logger, vdb)
		Expect(r.(*UpgradeOperatorReconciler).SetTLSEnabled(ctx, []appsv1.StatefulSet{*sts})).Should(Succeed())

		// Fetch the updated vdb to check the fields remain nil
		updatedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), updatedVdb)).Should(Succeed())
		Expect(updatedVdb.Spec.HTTPSNMATLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.HTTPSNMATLS.Enabled).Should(BeNil())
		Expect(updatedVdb.Spec.ClientServerTLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.ClientServerTLS.Enabled).Should(BeNil())
	})

	It("should do nothing if TLS enabled fields are already set", func() {
		vdb := vapi.MakeVDB()
		trueVal := true
		vdb.Spec.HTTPSNMATLS = &vapi.TLSConfigSpec{Enabled: &trueVal}
		vdb.Spec.ClientServerTLS = &vapi.TLSConfigSpec{Enabled: &trueVal}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		sc := &vdb.Spec.Subclusters[0]
		nm := names.GenStsName(vdb, sc)
		sts := builder.BuildStsSpec(nm, vdb, sc, "")
		sts.Labels[vmeta.OperatorVersionLabel] = "25.3.0" // older than 25.4.0

		Expect(k8sClient.Create(ctx, sts)).Should(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, sts)
		}()

		r := MakeUpgradeOperatorReconciler(vdbRec, logger, vdb)
		Expect(r.(*UpgradeOperatorReconciler).SetTLSEnabled(ctx, []appsv1.StatefulSet{*sts})).Should(Succeed())

		// Fields should remain unchanged
		updatedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), updatedVdb)).Should(Succeed())
		Expect(updatedVdb.Spec.HTTPSNMATLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.HTTPSNMATLS.Enabled).ShouldNot(BeNil())
		Expect(*updatedVdb.Spec.HTTPSNMATLS.Enabled).Should(BeTrue())
		Expect(updatedVdb.Spec.ClientServerTLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.ClientServerTLS.Enabled).ShouldNot(BeNil())
		Expect(*updatedVdb.Spec.ClientServerTLS.Enabled).Should(BeTrue())
	})

	It("should skip statefulsets without operator version label", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPSNMATLS = &vapi.TLSConfigSpec{}
		vdb.Spec.ClientServerTLS = &vapi.TLSConfigSpec{}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		nm := names.GenStsName(vdb, sc)
		sts := builder.BuildStsSpec(nm, vdb, sc, "")
		// Do not set operator version label
		delete(sts.Labels, vmeta.OperatorVersionLabel)

		Expect(k8sClient.Create(ctx, sts)).Should(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, sts)
		}()

		r := MakeUpgradeOperatorReconciler(vdbRec, logger, vdb)
		Expect(r.(*UpgradeOperatorReconciler).SetTLSEnabled(ctx, []appsv1.StatefulSet{*sts})).Should(Succeed())

		updatedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), updatedVdb)).Should(Succeed())
		Expect(updatedVdb.Spec.HTTPSNMATLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.HTTPSNMATLS.Enabled).Should(BeNil())
		Expect(updatedVdb.Spec.ClientServerTLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.ClientServerTLS.Enabled).Should(BeNil())
	})

	It("should skip statefulsets with unparsable operator version label", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPSNMATLS = &vapi.TLSConfigSpec{}
		vdb.Spec.ClientServerTLS = &vapi.TLSConfigSpec{}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		nm := names.GenStsName(vdb, sc)
		sts := builder.BuildStsSpec(nm, vdb, sc, "")
		sts.Labels[vmeta.OperatorVersionLabel] = "not-a-version"

		Expect(k8sClient.Create(ctx, sts)).Should(Succeed())
		defer func() {
			_ = k8sClient.Delete(ctx, sts)
		}()

		r := MakeUpgradeOperatorReconciler(vdbRec, logger, vdb)
		Expect(r.(*UpgradeOperatorReconciler).SetTLSEnabled(ctx, []appsv1.StatefulSet{*sts})).Should(Succeed())

		updatedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), updatedVdb)).Should(Succeed())
		Expect(updatedVdb.Spec.HTTPSNMATLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.HTTPSNMATLS.Enabled).Should(BeNil())
		Expect(updatedVdb.Spec.ClientServerTLS).ShouldNot(BeNil())
		Expect(updatedVdb.Spec.ClientServerTLS.Enabled).Should(BeNil())
	})
})
