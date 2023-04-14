/*
 (c) Copyright [2021-2022] Open Text.
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

package httpconf

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("file_writer", func() {
	ctx := context.Background()

	It("should fail if secret does not exist", func() {
		fwrt := FileWriter{}
		_, err := fwrt.GenConf(ctx, k8sClient, types.NamespacedName{Name: "does-not-exist", Namespace: "default"})
		Expect(err).ShouldNot(Succeed())
	})

	It("should succeed only if the secret has the correct keys", func() {
		secretName := types.NamespacedName{Name: "s1", Namespace: "default"}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName.Name,
				Namespace: secretName.Namespace,
			},
		}
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, secret)).Should(Succeed()) }()

		fwrt := FileWriter{}

		// None of the keys are set
		_, err := fwrt.GenConf(ctx, k8sClient, secretName)
		Expect(err).ShouldNot(Succeed())

		// Only tls.key is set
		secret.Data = map[string][]byte{corev1.TLSPrivateKeyKey: []byte("pk")}
		Expect(k8sClient.Update(ctx, secret)).Should(Succeed())
		_, err = fwrt.GenConf(ctx, k8sClient, secretName)
		Expect(err).ShouldNot(Succeed())

		// Only tls.key, tls.crt is set
		secret.Data[corev1.TLSCertKey] = []byte("crt")
		Expect(k8sClient.Update(ctx, secret)).Should(Succeed())
		_, err = fwrt.GenConf(ctx, k8sClient, secretName)
		Expect(err).ShouldNot(Succeed())

		// All keys are set
		secret.Data[paths.HTTPServerCACrtName] = []byte("ca")
		Expect(k8sClient.Update(ctx, secret)).Should(Succeed())
		fname, err := fwrt.GenConf(ctx, k8sClient, secretName)
		Expect(err).Should(Succeed())
		Expect(fname).ShouldNot(Equal(""))
		Expect(os.Remove(fname)).Should(Succeed())
	})
})
