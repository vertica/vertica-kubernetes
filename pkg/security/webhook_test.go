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

package security

import (
	"context"
	"os"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const prefixName = "verticadb-operator"
const ns = "default"

var _ = Describe("webhook", func() {
	ctx := context.Background()

	It("should update webhook configuration with given cert", func() {
		createWebhookConfiguration(ctx)
		defer deleteWebhookConfiguration(ctx)

		caCrt := []byte("==== CERT ====")
		Expect(PatchWebhookCABundle(ctx, &logger, restCfg, caCrt, prefixName, ns)).Should(Succeed())
		verifyCABundleEquals(ctx, prefixName, ns, caCrt)
	})

	It("should update webhook configuration with cert in given secret", func() {
		createWebhookConfiguration(ctx)
		defer deleteWebhookConfiguration(ctx)

		const secretName = "my-secret"
		var mockCert = []byte("my-cert")
		createSecret(ctx, secretName, map[string][]byte{CACertKey: mockCert})
		defer deleteSecret(ctx, secretName)

		Expect(PatchWebhookCABundleFromSecret(ctx, &logger, restCfg, secretName, prefixName, ns)).Should(Succeed())
		verifyCABundleEquals(ctx, prefixName, ns, mockCert)
	})

	It("should be a no-op if updating webhook configuration by cert is missing", func() {
		createWebhookConfiguration(ctx)
		defer deleteWebhookConfiguration(ctx)

		const secretName = "my-secret"
		createSecret(ctx, secretName, map[string][]byte{"key": []byte("val")})
		defer deleteSecret(ctx, secretName)

		Expect(PatchWebhookCABundleFromSecret(ctx, &logger, restCfg, secretName, prefixName, ns)).Should(Succeed())
		verifyCABundleEquals(ctx, prefixName, ns, nil)
	})

	It("should write out certs to a file", func() {
		createWebhookConfiguration(ctx)
		defer deleteWebhookConfiguration(ctx)

		dir, err := os.MkdirTemp("", "mock-cert")
		Expect(err).Should(Succeed())
		defer os.RemoveAll(dir)

		Expect(GenerateWebhookCert(ctx, &logger, restCfg, dir, prefixName, ns)).Should(Succeed())
		files, err := os.ReadDir(dir)
		Expect(err).Should(Succeed())
		Expect(len(files)).Should(Equal(2))
		sort.Slice(files, func(i, j int) bool {
			return files[i].Name() < files[j].Name()
		})
		Expect(files[0].Name()).Should(Equal(corev1.TLSCertKey))
		Expect(files[1].Name()).Should(Equal(corev1.TLSPrivateKeyKey))
	})
})

func createWebhookConfiguration(ctx context.Context) {
	sideEffects := admissionregistrationv1.SideEffectClassNone
	host := "https://127.0.0.1"
	validatingCfg := admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: getValidatingWebhookConfigName(prefixName, ns),
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				AdmissionReviewVersions: []string{"v1"},
				Name:                    "vcrd.kb.io",
				SideEffects:             &sideEffects,
				ClientConfig:            admissionregistrationv1.WebhookClientConfig{URL: &host},
			},
		},
	}
	Expect(k8sClient.Create(ctx, &validatingCfg)).Should(Succeed())
	mutatingCfg := admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: getMutatingWebhookConfigName(prefixName, ns),
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				AdmissionReviewVersions: []string{"v1"},
				Name:                    "mcrd.kb.io",
				SideEffects:             &sideEffects,
				ClientConfig:            admissionregistrationv1.WebhookClientConfig{URL: &host},
			},
		},
	}
	Expect(k8sClient.Create(ctx, &mutatingCfg)).Should(Succeed())
}

func deleteWebhookConfiguration(ctx context.Context) {
	nm := types.NamespacedName{
		Name: getValidatingWebhookConfigName(prefixName, ns),
	}
	vcfg := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	Expect(k8sClient.Get(ctx, nm, vcfg)).Should(Succeed())
	Expect(k8sClient.Delete(ctx, vcfg)).Should(Succeed())
	nm = types.NamespacedName{
		Name: getMutatingWebhookConfigName(prefixName, ns),
	}
	mcfg := &admissionregistrationv1.MutatingWebhookConfiguration{}
	Expect(k8sClient.Get(ctx, nm, mcfg)).Should(Succeed())
	Expect(k8sClient.Delete(ctx, mcfg)).Should(Succeed())
}

func createSecret(ctx context.Context, secretName string, data map[string][]byte) {
	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ns,
		},
		Data: data,
	}
	Expect(k8sClient.Create(ctx, &secret)).Should(Succeed())
}

func deleteSecret(ctx context.Context, secretName string) {
	nm := types.NamespacedName{
		Namespace: ns,
		Name:      secretName,
	}
	secret := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, nm, secret)).Should(Succeed())
	Expect(k8sClient.Delete(ctx, secret)).Should(Succeed())
}

func verifyCABundleEquals(ctx context.Context, prefixName, ns string, caCrt []byte) {
	nm := types.NamespacedName{
		Name: getValidatingWebhookConfigName(prefixName, ns),
	}
	vcfg := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	Expect(k8sClient.Get(ctx, nm, vcfg)).Should(Succeed())
	if len(caCrt) > 0 {
		Expect(len(vcfg.Webhooks)).Should(Equal(1))
		Expect(len(vcfg.Webhooks[0].ClientConfig.CABundle)).Should(BeNumerically(">", 0))
	} else {
		Expect(len(vcfg.Webhooks[0].ClientConfig.CABundle)).Should(Equal(0))
	}
	Expect(vcfg.Webhooks[0].ClientConfig.CABundle).Should(Equal(caCrt))
	nm = types.NamespacedName{
		Name: getMutatingWebhookConfigName(prefixName, ns),
	}
	mcfg := &admissionregistrationv1.MutatingWebhookConfiguration{}
	Expect(k8sClient.Get(ctx, nm, mcfg)).Should(Succeed())
	Expect(len(mcfg.Webhooks)).Should(Equal(1))
	if len(caCrt) > 0 {
		Expect(len(mcfg.Webhooks[0].ClientConfig.CABundle)).Should(BeNumerically(">", 0))
		Expect(mcfg.Webhooks[0].ClientConfig.CABundle).Should(Equal(caCrt))
	} else {
		Expect(len(mcfg.Webhooks[0].ClientConfig.CABundle)).Should(Equal(0))
	}
}
