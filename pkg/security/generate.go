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
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
)

const pkKeySize = 2048

// NewSelfSignedCACertificate creates and returns a CA certificate
func NewSelfSignedCACertificate() (Certificate, error) {
	// Create the private key
	caPK, err := rsa.GenerateKey(rand.Reader, pkKeySize)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create private key")
	}

	// Create the CA cert
	caCrt := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Open Text"}, Country: []string{"US"}, OrganizationalUnit: []string{"Vertica"}, CommonName: "rootca",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caCert, err := x509.CreateCertificate(rand.Reader, caCrt, caCrt, &caPK.PublicKey, caPK)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create CA certificate")
	}

	return &certificate{
		tlsKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caPK)}),
		tlsCrt: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert}),
	}, nil
}

// NewCertificateWithIPs will create a certificate using the given CA.
func NewCertificateWithIPs(ca Certificate, commonName string, dnsNames []string, ips []net.IP) (Certificate, error) {
	caCrt, err := ca.Buildx509()
	if err != nil {
		return nil, err
	}

	caPK, err := ca.BuildPrivateKey()
	if err != nil {
		return nil, err
	}

	// Create the private key
	pk, err := rsa.GenerateKey(rand.Reader, pkKeySize)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create private key")
	}

	crt := newx509Certificate(commonName, dnsNames, ips)
	keyCert, err := x509.CreateCertificate(rand.Reader, crt, caCrt, &pk.PublicKey, caPK)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create certificate")
	}

	return &certificate{
		tlsKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(pk)}),
		tlsCrt: pem.EncodeToMemory((&pem.Block{Type: "CERTIFICATE", Bytes: keyCert})),
	}, nil
}

// NewCertificate will create a certificate using the given CA.
func NewCertificate(ca Certificate, commonName string, dnsNames []string) (Certificate, error) {
	return NewCertificateWithIPs(ca, commonName, dnsNames, nil)
}

func newx509Certificate(commonName string, dnsNames []string, ips []net.IP) *x509.Certificate {
	crt := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:       []string{"Open Text"},
			Country:            []string{"US"},
			OrganizationalUnit: []string{"Vertica"},
			CommonName:         commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  false,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		DNSNames:              dnsNames,
		BasicConstraintsValid: true,
	}

	if len(ips) != 0 {
		crt.IPAddresses = append(crt.IPAddresses, ips...)
	}

	return crt
}

func GetDNSNames(namespace string) []string {
	return []string{
		fmt.Sprintf("*.%s.svc", namespace),
		fmt.Sprintf("*.%s.svc.cluster.local", namespace),
	}
}

func GenSecret(secretName, namespace string, cert, caCert Certificate) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      secretName,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey:   cert.TLSKey(),
			corev1.TLSCertKey:         cert.TLSCrt(),
			paths.HTTPServerCACrtName: caCert.TLSCrt(),
		},
	}
}

// Certificate for unit tests, with customizeable fields
func NewTestCertificate(ca Certificate, commonName string, dnsNames []string, ips []net.IP,
	notBefore, notAfter time.Time,
	extUsages []x509.ExtKeyUsage,
	keyUsage x509.KeyUsage,
	isCA bool,
) (Certificate, error) {
	caCrt, err := ca.Buildx509()
	if err != nil {
		return nil, err
	}
	caPK, err := ca.BuildPrivateKey()
	if err != nil {
		return nil, err
	}

	pk, err := rsa.GenerateKey(rand.Reader, pkKeySize)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create private key")
	}

	// Get SubjectKeyId
	pubKeyBytes, _ := x509.MarshalPKIXPublicKey(&pk.PublicKey)
	ski := sha256.Sum256(pubKeyBytes)

	crt := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:       []string{"Open Text"},
			Country:            []string{"US"},
			OrganizationalUnit: []string{"Vertica"},
			CommonName:         commonName,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  isCA,
		ExtKeyUsage:           extUsages,
		KeyUsage:              keyUsage,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
		BasicConstraintsValid: true,
		SubjectKeyId:          ski[:],
	}

	keyCert, err := x509.CreateCertificate(rand.Reader, crt, caCrt, &pk.PublicKey, caPK)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create custom certificate")
	}

	return &certificate{
		tlsKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(pk)}),
		tlsCrt: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: keyCert}),
	}, nil
}
