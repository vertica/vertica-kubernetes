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
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("rollbackaftercertrotation_reconciler", func() {
	ctx := context.Background()

	It("should be a no-op if TLS cert rollback is disabled", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations = map[string]string{}

		vdb.Annotations[vmeta.DisableTLSRotationFailureRollbackAnnotation] = vmeta.DisableTLSRotationFailureRollbackAnnotationTrue
		cond := vapi.MakeCondition(vapi.TLSCertRollbackNeeded, metav1.ConditionTrue, vapi.RollbackAfterHTTPSCertRotationReason)
		meta.SetStatusCondition(&vdb.Status.Conditions, *cond)

		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, &cmds.FakePodRunner{}, &testPassword)
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, &testPassword)

		recon := MakeRollbackAfterCertRotationReconciler(vdbRec, logger, vdb, dispatcher, &pfacts)
		res, err := recon.Reconcile(ctx, nil)

		Expect(err).ShouldNot(HaveOccurred())
		Expect(res.Requeue).To(BeFalse())

		cond = meta.FindStatusCondition(vdb.Status.Conditions, vapi.TLSCertRollbackInProgress)
		Expect(cond).To(BeNil(), "Expected TLSCertRollbackInProgress condition to not be set")
	})

	It("should be a no-op if TLS cert rollback is not required", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations = map[string]string{}

		vdb.Annotations[vmeta.DisableTLSRotationFailureRollbackAnnotation] = vmeta.DisableTLSRotationFailureRollbackAnnotationFalse
		cond := vapi.MakeCondition(vapi.TLSCertRollbackNeeded, metav1.ConditionFalse, "")
		meta.SetStatusCondition(&vdb.Status.Conditions, *cond)

		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, &cmds.FakePodRunner{}, &testPassword)
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, &testPassword)

		recon := MakeRollbackAfterCertRotationReconciler(vdbRec, logger, vdb, dispatcher, &pfacts)
		res, err := recon.Reconcile(ctx, nil)

		Expect(err).ShouldNot(HaveOccurred())
		Expect(res.Requeue).To(BeFalse())

		cond = meta.FindStatusCondition(vdb.Status.Conditions, vapi.TLSCertRollbackInProgress)
		Expect(cond).To(BeNil(), "Expected TLSCertRollbackInProgress condition to not be set")
	})

})
