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

package httpconf

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FileWriter is a writer for httpstls.json
type FileWriter struct {
	Conf HTTPSTLSConf
}

// GenConf will generate the TLS conf and write it out to disk.  The name of the
// file will be returned.  The pod that it gets written too is specified by TargetPod.
func (f *FileWriter) GenConf(ctx context.Context, cli client.Client, tlsSecretName types.NamespacedName) (string, error) {
	secret := &corev1.Secret{}
	if err := cli.Get(ctx, tlsSecretName, secret); err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("failed to fetch the TLS secret '%s", tlsSecretName))
	}
	if err := f.buildConfInMemory(secret); err != nil {
		return "", errors.Wrap(err, "failed to build the TLS conf in memory")
	}
	fileName, err := f.writeConf()
	if err != nil {
		return "", errors.Wrap(err, "failed to write the TLS conf")
	}
	return fileName, nil
}

// buildConfInMemory will build up the config file in memory (in f.Conf)
func (f *FileWriter) buildConfInMemory(tlsSecret *corev1.Secret) error {
	// Set the first couple of fields that are hardcoded.  The values here are
	// originally take from the install_vertica app.
	f.Conf.Name = "server"
	f.Conf.CipherSuites = ""
	const TLSVerifyModeTryVerify = 2
	f.Conf.Mode = TLSVerifyModeTryVerify
	f.Conf.ChainCerts = []string{}

	tlsKey, ok := tlsSecret.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return fmt.Errorf("key %s is missing in the secret %s", corev1.TLSPrivateKeyKey, tlsSecret.Name)
	}
	f.Conf.Key = string(tlsKey)

	tlsCrt, ok := tlsSecret.Data[corev1.TLSCertKey]
	if !ok {
		return fmt.Errorf("key %s is missing in the secret %s", corev1.TLSCertKey, tlsSecret.Name)
	}
	f.Conf.Certificate = string(tlsCrt)

	caCrt, ok := tlsSecret.Data[paths.HTTPServerCACrtName]
	if !ok {
		return fmt.Errorf("key %s is missing in the secret %s", paths.HTTPServerCACrtName, tlsSecret.Name)
	}
	f.Conf.CACerts = []string{string(caCrt)}

	return nil
}

// writeConf will write the contents of f.Conf to a file. The file name is
// returned. It is the callers responsibility to remove the file if it is a
// temporary.
func (f *FileWriter) writeConf() (string, error) {
	tmp, err := os.CreateTemp("", "httpstls.conf.")
	if err != nil {
		return "", errors.Wrap(err, "failed to create temporary file for httpstls.conf")
	}
	defer tmp.Close()

	jConf, err := json.Marshal(f.Conf)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshall the in-memory representation of the config file")
	}

	_, err = tmp.Write(jConf)
	if err != nil {
		return "", errors.Wrap(err, "failed to write conf to file")
	}

	return tmp.Name(), nil
}
