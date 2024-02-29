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

package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	v1vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

const (
	CACertKey                 = "ca.crt"
	OLMCACertKey              = "olmCAKey"
	certManagerAnnotationName = "cert-manager.io/inject-ca-from"
)

// PatchWebhookCABundle will update the webhook configuration with the given CA cert.
func PatchWebhookCABundle(ctx context.Context, log *logr.Logger, cfg *rest.Config, caCert []byte, prefixName, namespace string) error {
	log.Info("Patching webhook configurations with CA bundle")
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "could not create kubernetes clientset")
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

	return patchConversionWebhookConfig(ctx, log, cfg, prefixName, namespace, nil, caCert)
}

// AddCertManagerAnnotation will annotate the CRD so that cert-manager can
// inject the CA for the conversion webhook.
func AddCertManagerAnnotation(ctx context.Context, log *logr.Logger, cfg *rest.Config, prefixName, namespace string) error {
	// We will set an annotation to allow cert-manager to inject the bundle. We
	// also need to setup the remainin parts of the conversion webhook for it to
	// function correctly.
	annotations := map[string]string{
		certManagerAnnotationName: fmt.Sprintf("%s/%s-serving-cert", namespace, prefixName),
	}
	return patchConversionWebhookConfig(ctx, log, cfg, prefixName, namespace, annotations, nil)
}

// PatchWebhookCABundleFromSecret will update the webhook configurations with the CA cert in the given secret.
func PatchWebhookCABundleFromSecret(ctx context.Context, log *logr.Logger, cfg *rest.Config, secretName, prefixName, ns string) error {
	caCrt, err := getCertFromSecret(ctx, log, cfg, secretName, ns)
	if caCrt == nil || err != nil {
		return err
	}
	return PatchWebhookCABundle(ctx, log, cfg, caCrt, prefixName, ns)
}

// PatchConversionWebhookFromSecret will only update the webhook conversion with
// the CA bundle from the given secret.
func PatchConversionWebhookFromSecret(ctx context.Context, log *logr.Logger, cfg *rest.Config, secretName, prefixName, ns string) error {
	caCrt, err := getCertFromSecret(ctx, log, cfg, secretName, ns)
	if caCrt == nil || err != nil {
		return err
	}
	return patchConversionWebhookConfig(ctx, log, cfg, prefixName, ns, nil, caCrt)
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

	return PatchWebhookCABundle(ctx, log, cfg, caCert.TLSCrt(), prefixName, ns)
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

func getCertFromSecret(ctx context.Context, log *logr.Logger, cfg *rest.Config, secretName, ns string) ([]byte, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "could not create config")
	}
	api := cs.CoreV1().Secrets(ns)
	secret, err := api.Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("could not fetch secret %s in namespace %s", secretName, ns))
	}
	if secret.Data == nil {
		log.Info("No data elems in the secret. Not updating CA bundle in webhook config.", "secret", secretName)
		return nil, nil
	}
	caCrt, ok := secret.Data[CACertKey]
	if !ok {
		// When deploying with OLM, the secret that is generated has a different
		// key value for the CA cert.
		caCrt, ok = secret.Data[OLMCACertKey]
	}
	if !ok {
		// If the secret doesn't have the necessary key, then we just skip the
		// patch. This is done for backwards compatibility where we previously
		// only required the CA bundle to be specified as a separate helm chart
		// parameter.
		log.Info("could not find key in secret. Not updating CA bundle in webhook config.",
			"key", CACertKey, "secret", secretName)
		return nil, nil
	}
	return caCrt, nil
}

// patchConversionWebhookConfig will update the CRD with the CA bundle for the
// webhook conversion endpoint. This conversion webhook is used to convert
// between the different versions of CRDs we have.
func patchConversionWebhookConfig(ctx context.Context, log *logr.Logger, cfg *rest.Config,
	prefixName, namespace string, annotations map[string]string, caCert []byte) error {
	cs, err := apiclientset.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "could not create apiextensions clientset")
	}

	crdName := getVerticaDBCRDName()
	api := cs.ApiextensionsV1().CustomResourceDefinitions()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		crd, err := api.Get(ctx, crdName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		// Generally the conversion webhook strategy should already be set in
		// the CRD. However, we can come in here for test purposes with a
		// strategy of None. So, we need to set it for that case.
		log.Info("Updating webhook conversion", "oldStrategy", crd.Spec.Conversion.Strategy)
		crd.Spec.Conversion.Strategy = extv1.WebhookConverter

		for k, v := range annotations {
			log.Info("Setting annotation in CRD", "key", k, "value", v)
			crd.Annotations[k] = v
		}

		webhookPath := "/convert"
		crd.Spec.Conversion.Webhook = &extv1.WebhookConversion{
			ClientConfig: &extv1.WebhookClientConfig{
				Service: &extv1.ServiceReference{
					Namespace: namespace,
					Name:      getWebhookServiceName(prefixName),
					Path:      &webhookPath,
				},
				CABundle: caCert,
			},
			ConversionReviewVersions: []string{
				v1beta1vapi.Version,
			},
		}
		// We set the caBundle if it was passed in. This is optional to allow
		// for injection from cert-manager.
		if caCert != nil {
			crd.Spec.Conversion.Webhook.ClientConfig.CABundle = caCert
		}
		_, err = api.Update(ctx, crd, metav1.UpdateOptions{})
		return err
	})
}

func getValidatingWebhookConfigName(prefixName string) string {
	return fmt.Sprintf("%s-validating-webhook-configuration", prefixName)
}

func getMutatingWebhookConfigName(prefixName string) string {
	return fmt.Sprintf("%s-mutating-webhook-configuration", prefixName)
}

// getWebhookServiceName will return the name of the webhook service object. It
// does not include the namespace.
func getWebhookServiceName(prefixName string) string {
	// We have slightly different names depending on the deployment type since
	// OLM likes to generate it themselves and tie the CA cert to it.
	if opcfg.GetIsOLMDeployment() {
		return fmt.Sprintf("%s-manager-service", prefixName)
	}
	return fmt.Sprintf("%s-webhook-service", prefixName)
}

// getVerticaDBCRDName returns the name of the CRD for VerticaDB
func getVerticaDBCRDName() string {
	return fmt.Sprintf("%s.%s", v1vapi.VerticaDBKindPlural, v1vapi.Group)
}
