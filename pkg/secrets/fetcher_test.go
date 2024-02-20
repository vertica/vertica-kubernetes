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

package secrets

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("secrets/fetcher", func() {
	ctx := context.Background()

	It("should read secret from k8s", func() {
		const SecretNamespace = "my-secret"
		k8sClient := makeStandardK8sClient()
		_, err := k8sClient.createNamespace(ctx, SecretNamespace)
		Ω(err).Should(Succeed())
		defer func() { Ω(k8sClient.deleteNamespace(ctx, SecretNamespace)) }()

		const SecretName = "secret-1"
		const SecretContent = "supersecret"
		secret := corev1.Secret{
			StringData: map[string]string{
				"password": SecretContent,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      SecretName,
				Namespace: SecretNamespace,
			},
		}
		nm := types.NamespacedName{
			Name:      SecretName,
			Namespace: SecretNamespace,
		}
		_, err = k8sClient.CreateSecret(ctx, nm, &secret)
		Ω(err).Should(Succeed())
		defer func() { Ω(k8sClient.DeleteSecret(ctx, nm)).Should(Succeed()) }()
		fetcher := MultiSourceSecretFetcher{
			Log:       logger,
			K8sClient: k8sClient,
		}
		data, err := fetcher.Fetch(ctx, types.NamespacedName{Namespace: SecretNamespace, Name: SecretName})
		Ω(err).Should(Succeed())
		Ω(string(data["password"])).Should(Equal(SecretContent))
	})

	It("should return secret not found if request one that doens't exist", func() {
		k8sClient := makeStandardK8sClient()
		fetcher := MultiSourceSecretFetcher{
			Log:       logger,
			K8sClient: k8sClient,
		}
		_, err := fetcher.Fetch(ctx, types.NamespacedName{Namespace: "default", Name: "not-exist"})
		Ω(err).ShouldNot(Succeed())
		nfe := &NotFoundError{}
		ok := errors.As(err, &nfe)
		Ω(ok).Should(BeTrue())
	})
})

func makeStandardK8sClient() *StandardK8sClient {
	return &StandardK8sClient{
		Config: config,
	}
}
