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

package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

const CACertKey = "ca.crt"

// PatchWebhookCABundle will update the webhook configuration with the given CA cert.
func PatchWebhookCABundle(ctx context.Context, log *logr.Logger, cfg *rest.Config, caCert []byte, prefixName string) error {
	log.Info("Patching webhook configurations with CA bundle")
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "could not create config")
	}
	cfgName := getMutatingWebhookConfigName(prefixName)
	err = patchMutatingWebhookConfig(ctx, cs, cfgName, caCert)
	if err != nil {
		return errors.Wrap(err, "failed to patch the mutating webhook cfg")
	}
	cfgName = getValidatingWebhookConfigName(prefixName)
	err = patchValidatingWebhookConfig(ctx, cs, cfgName, caCert)
	if err != nil {
		return errors.Wrap(err, "failed to patch the mutating webhook cfg")
	}
	return nil
}

// PatchWebhookCABundleFromSecret will update the webhook configurations with the CA cert in the given secret.
func PatchWebhookCABundleFromSecret(ctx context.Context, log *logr.Logger, cfg *rest.Config, secretName, prefixName, ns string) error {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "could not create config")
	}
	api := cs.CoreV1().Secrets(ns)
	secret, err := api.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("could not fetch secret %s in namespace %s", secretName, ns))
	}
	if secret.Data == nil {
		log.Info("No data elems in the secret. Not updating CA bundle in webhook config.", "secret", secretName)
		return nil
	}
	caCrt, ok := secret.Data[CACertKey]
	if !ok {
		// If the secret doesn't have the necessary key, then we just skip the
		// patch. This is done for backwards compatibility where we previously
		// only required the CA bundle to be specified as a separate helm chart
		// parameter.
		log.Info("could not find key in secret. Not updating CA bundle in webhook config.",
			"key", CACertKey, "secret", secretName)
		return nil
	}
	return PatchWebhookCABundle(ctx, log, cfg, caCrt, prefixName)
}

// GenerateWebhookCert will create the cert to be used by the webhook. On success, this
// will have created the certs in the cert directory (CertDir). This is only
// called when deploying the operator and they have chosen that an internal
// self-signed cert is used.
func GenerateWebhookCert(ctx context.Context, log *logr.Logger, cfg *rest.Config, certDir, prefixName, ns string) error {
	log.Info("Generating cert for webhook")
	const PKKeySize = 1024
	caCert, err := NewSelfSignedCACertificate(PKKeySize)
	if err != nil {
		return errors.Wrap(err, "could not create self-signed CA for webhook")
	}
	dnsNames := []string{
		fmt.Sprintf("%s-webhook-service.%s.svc", prefixName, ns),
		fmt.Sprintf("%s-webhook-service.%s.svc.cluster.local", prefixName, ns),
	}
	cert, err := NewCertificate(caCert, PKKeySize, dnsNames[0], dnsNames)
	if err != nil {
		return errors.Wrap(err, "could not create webhook cert")
	}
	err = writeCert(certDir, cert)
	if err != nil {
		return errors.Wrap(err, "could not write out cert")
	}

	return PatchWebhookCABundle(ctx, log, cfg, caCert.TLSCrt(), prefixName)
}

func writeCert(certDir string, cert Certificate) error {
	if err := os.MkdirAll(certDir, os.ModePerm); err != nil {
		return errors.Wrap(err, "failed to create certs dir")
	}

	const CertAccessMode = 0600
	mode := os.FileMode(CertAccessMode)
	if err := os.WriteFile(filepath.Join(certDir, corev1.TLSCertKey), cert.TLSCrt(), mode); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to write %s", corev1.TLSCertKey))
	}
	if err := os.WriteFile(filepath.Join(certDir, corev1.TLSPrivateKeyKey), cert.TLSKey(), mode); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to write %s", corev1.TLSPrivateKeyKey))
	}

	return nil
}

//nolint:dupl
func patchMutatingWebhookConfig(ctx context.Context, cs *kubernetes.Clientset, cfgName string, caCert []byte) error {
	api := cs.AdmissionregistrationV1().MutatingWebhookConfigurations()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cfg, err := api.Get(ctx, cfgName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for i := range cfg.Webhooks {
			cfg.Webhooks[i].ClientConfig.CABundle = caCert
		}
		_, err = api.Update(ctx, cfg, metav1.UpdateOptions{})
		return err
	})
}

//nolint:dupl
func patchValidatingWebhookConfig(ctx context.Context, cs *kubernetes.Clientset, cfgName string, caCert []byte) error {
	api := cs.AdmissionregistrationV1().ValidatingWebhookConfigurations()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cfg, err := api.Get(ctx, cfgName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		for i := range cfg.Webhooks {
			cfg.Webhooks[i].ClientConfig.CABundle = caCert
		}
		_, err = api.Update(ctx, cfg, metav1.UpdateOptions{})
		return err
	})
}

func getValidatingWebhookConfigName(prefixName string) string {
	return fmt.Sprintf("%s-validating-webhook-configuration", prefixName)
}

func getMutatingWebhookConfigName(prefixName string) string {
	return fmt.Sprintf("%s-mutating-webhook-configuration", prefixName)
}
