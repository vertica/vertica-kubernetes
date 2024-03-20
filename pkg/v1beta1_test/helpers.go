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

package v1beta1_test

import (
	"context"

	. "github.com/onsi/gomega" //nolint:stylecheck
	v1vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreatePods(ctx context.Context, c client.Client, vdb *vapi.VerticaDB, podRunningState test.PodRunningState) {
	v1vdb := v1vapi.VerticaDB{}
	err := vdb.ConvertTo(&v1vdb)
	Expect(err).Should(Succeed())
	test.CreatePods(ctx, c, &v1vdb, podRunningState)
}

func DeletePods(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	v1vdb := v1vapi.VerticaDB{}
	err := vdb.ConvertTo(&v1vdb)
	Expect(err).Should(Succeed())
	test.DeletePods(ctx, c, &v1vdb)
}

func CreateVAS(ctx context.Context, c client.Client, vas *vapi.VerticaAutoscaler) {
	ExpectWithOffset(1, c.Create(ctx, vas)).Should(Succeed())
}

func DeleteVAS(ctx context.Context, c client.Client, vas *vapi.VerticaAutoscaler) {
	ExpectWithOffset(1, c.Delete(ctx, vas)).Should(Succeed())
}

func CreateVDB(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	ExpectWithOffset(1, c.Create(ctx, vdb)).Should(Succeed())
}

func DeleteVDB(ctx context.Context, c client.Client, vdb *vapi.VerticaDB) {
	ExpectWithOffset(1, c.Delete(ctx, vdb)).Should(Succeed())
}

func CreateVSCR(ctx context.Context, c client.Client, vscr *vapi.VerticaScrutinize) {
	ExpectWithOffset(1, c.Create(ctx, vscr)).Should(Succeed())
}

func DeleteVSCR(ctx context.Context, c client.Client, vscr *vapi.VerticaScrutinize) {
	ExpectWithOffset(1, c.Delete(ctx, vscr)).Should(Succeed())
}

func CreateScrutinizePod(ctx context.Context, c client.Client, vscr *vapi.VerticaScrutinize) {
	vdb := v1vapi.MakeVDB()
	pod := builder.BuildScrutinizePod(vscr, vdb, []string{
		"--tarball-name", "test",
	})
	ExpectWithOffset(1, c.Create(ctx, pod)).Should(Succeed())
}

func DeleteScrutinizePod(ctx context.Context, c client.Client, vscr *vapi.VerticaScrutinize) {
	pod := &corev1.Pod{}
	err := c.Get(ctx, vscr.ExtractNamespacedName(), pod)
	if !kerrors.IsNotFound(err) {
		ExpectWithOffset(1, c.Delete(ctx, pod)).Should(Succeed())
	}
}
