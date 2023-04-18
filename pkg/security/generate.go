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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/pkg/errors"
)

// NewSelfSignedCACertificate creates and returns a CA certificate
func NewSelfSignedCACertificate(keySize int) (Certificate, error) {
	// Create the private key
	caPK, err := rsa.GenerateKey(rand.Reader, keySize)
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

// NewCertificate will create a certificate using the given CA.
func NewCertificate(ca Certificate, keySize int, commonName string, dnsNames []string) (Certificate, error) {
	caCrt, err := ca.Buildx509()
	if err != nil {
		return nil, err
	}

	caPK, err := ca.BuildPrivateKey()
	if err != nil {
		return nil, err
	}

	// Create the private key
	pk, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create private key")
	}

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
	keyCert, err := x509.CreateCertificate(rand.Reader, crt, caCrt, &pk.PublicKey, caPK)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create certificate")
	}

	return &certificate{
		tlsKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(pk)}),
		tlsCrt: pem.EncodeToMemory((&pem.Block{Type: "CERTIFICATE", Bytes: keyCert})),
	}, nil
}
