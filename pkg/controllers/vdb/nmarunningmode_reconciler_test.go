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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("k8s/nmarunningmode_reconcile", func() {
	ctx := context.Background()

	It("should fail the reconclier if we configure the NMA to run in a sidecar container", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Annotations[vmeta.RunNMAInSidecarAnnotation] = vmeta.RunNMAInSidecarAnnotationFalse

		n := MakeNMARunningModeReconciler(vdbRec, logger, vdb)

		// running NMA in monolithic container, currently supported
		res, err := n.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		vdb.Annotations[vmeta.RunNMAInSidecarAnnotation] = vmeta.RunNMAInSidecarAnnotationTrue
		// running NMA in sidecar container, currently not supported
		res, err = n.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(MatchError(fmt.Errorf("running NMA in a sidecar container is not supported yet")))

		delete(vdb.Annotations, vmeta.RunNMAInSidecarAnnotation)
		// test the default, which is running NMA in sidecar container, currently not supported
		res, err = n.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(MatchError(fmt.Errorf("running NMA in a sidecar container is not supported yet")))
	})
})
