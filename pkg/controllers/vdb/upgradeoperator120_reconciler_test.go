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

package vdb

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("k8s/upgradeoperator120_reconciler", func() {
	ctx := context.Background()

	It("should delete sts that was created prior to current release", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		nm := names.GenStsName(vdb, sc)
		sts := builder.BuildStsSpec(nm, vdb, sc, builder.DefaultServiceAccountName)
		// Set an old operator version to force the upgrade
		sts.Labels[builder.OperatorVersionLabel] = builder.OperatorVersion110
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

		r := MakeUpgradeOperator120Reconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		// Reconcile should have deleted the sts because it was created by an
		// old operator version
		Expect(k8sClient.Get(ctx, nm, fetchedSts)).ShouldNot(Succeed())
	})
})
