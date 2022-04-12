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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("k8s", func() {
	ctx := context.Background()

	It("should be able to fetch a secret", func() {
		vdb := vapi.MakeVDB()
		nm := names.GenNamespacedName(vdb, "secret1")
		secret := corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nm.Name,
				Namespace: nm.Namespace,
			},
			Data: map[string][]byte{
				"Data1": []byte("secret"),
			},
		}
		Expect(k8sClient.Create(ctx, &secret)).Should(Succeed())
		defer deleteSecret(ctx, vdb, nm.Name)

		fetchSecret, res, err := getSecret(ctx, vdbRec, vdb, nm)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(fetchSecret.Data["Data1"]).Should(Equal([]byte("secret")))
	})

	It("should be able to fetch a configMap", func() {
		vdb := vapi.MakeVDB()
		nm := names.GenNamespacedName(vdb, "cm1")
		cm := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nm.Name,
				Namespace: nm.Namespace,
			},
			Data: map[string]string{
				"cmData1": "stuff",
				"cmData2": "things",
			},
		}
		Expect(k8sClient.Create(ctx, &cm)).Should(Succeed())
		defer deleteConfigMap(ctx, vdb, nm.Name)

		fetchCm, res, err := getConfigMap(ctx, vdbRec, vdb, nm)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(fetchCm.Data["cmData1"]).Should(Equal("stuff"))
		Expect(fetchCm.Data["cmData2"]).Should(Equal("things"))
	})
})
