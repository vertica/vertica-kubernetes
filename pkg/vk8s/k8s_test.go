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

package vk8s

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mockVRec struct{}

func (m mockVRec) Event(vdb runtime.Object, eventType, reason, message string) {}
func (m mockVRec) Eventf(vdb runtime.Object, eventType, reason, messageFmt string, args ...interface{}) {
}
func (m mockVRec) GetClient() client.Client { return k8sClient }
func (m mockVRec) GetEventRecorder() record.EventRecorder {
	return mgr.GetEventRecorderFor(vmeta.OperatorName)
}
func (m mockVRec) GetConfig() *rest.Config { return nil }

var _ = Describe("vk8s/k8s", func() {
	ctx := context.Background()

	It("should be able to update the vdb with a callback", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrec := mockVRec{}
		updated, err := UpdateVDBWithRetry(ctx, vrec, vdb, func() (bool, error) {
			vdb.Spec.Subclusters[0].Size = 99
			return true, nil
		})
		Ω(updated).Should(BeTrue())
		Ω(err).Should(Succeed())
		Ω(vdb.Spec.Subclusters[0].Size).Should(Equal(int32(99)))
		fetchVDB := vapi.VerticaDB{}
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), &fetchVDB)).Should(Succeed())
		Ω(fetchVDB.Spec.Subclusters[0].Size).Should(Equal(int32(99)))
	})

	It("fetch of a non-existent vdb should generate an event", func() {
		vdb := vapi.MakeVDB()
		vrec := mockVRec{}
		Ω(FetchVDB(ctx, vrec, vdb, vdb.ExtractNamespacedName(), vdb)).Should(Equal(ctrl.Result{Requeue: true}))

		vdb = vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		Ω(FetchVDB(ctx, vrec, vdb, vdb.ExtractNamespacedName(), vdb)).Should(Equal(ctrl.Result{}))
	})
})
