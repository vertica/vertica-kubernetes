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
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"github.com/pkg/errors"
)

type Certificate interface {
	TLSKey() []byte
	TLSCrt() []byte
	Buildx509() (*x509.Certificate, error)
	BuildPrivateKey() (*rsa.PrivateKey, error)
}

type certificate struct {
	tlsKey []byte
	tlsCrt []byte
}

func (c *certificate) TLSKey() []byte { return c.tlsKey }
func (c *certificate) TLSCrt() []byte { return c.tlsCrt }

func (c *certificate) Buildx509() (*x509.Certificate, error) {
	block, _ := pem.Decode(c.TLSCrt())
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse certificate")
	}
	return cert, nil
}

func (c *certificate) BuildPrivateKey() (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(c.TLSKey())
	if block == nil {
		return nil, fmt.Errorf("failed to decode private key")
	}

	pk, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse certificate")
	}
	return pk, nil
}
