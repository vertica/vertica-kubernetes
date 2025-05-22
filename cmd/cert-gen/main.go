/*
 (c) Copyright [2021-2025] Open Text.

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

package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/vertica/vertica-kubernetes/pkg/certgen"
	"github.com/vertica/vertica-kubernetes/pkg/security"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
)

const (
	SecretNameArg = iota
	NamespaceArg
	CommonNameArg
	NumPositionalArgs
)

// Base64 encode secret Data for YAML output
func encodeSecretData(secret *corev1.Secret) map[string]string {
	encoded := make(map[string]string)
	for k, v := range secret.Data {
		encoded[k] = base64.StdEncoding.EncodeToString(v)
	}
	return encoded
}

func usage() {
	fmt.Printf("Usage: %s [OPTIONS] <secret-name> <secret-namespace> <common-name>\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	opts := certgen.Options{}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flag.Usage = usage
	flag.StringVar(&opts.DNSNames, "extra-dns-names", "",
		"Comma-separated list of dns names that will be included in the certificate's Subject Alternative Name (SAN) extension.")
	flag.StringVar(&opts.IPAddresses, "ips", "",
		"Comma-separated list of ip addresses that will be included in the certificate's Subject Alternative Name (SAN) extension.")
	flag.Parse()

	if flag.NArg() < NumPositionalArgs {
		fmt.Println(flag.NArg())
		fmt.Println("Not enough positional arguments.")
		flag.Usage()
		os.Exit(1)
	}
	opts.SecretName = flag.Arg(SecretNameArg)
	opts.Namespace = flag.Arg(NamespaceArg)
	opts.CommonName = flag.Arg(CommonNameArg)

	var ipAddresses []net.IP
	if opts.IPAddresses != "" {
		ips, err := certgen.ParseAndValidateIPs(opts.IPAddresses)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ipAddresses = append(ipAddresses, ips...)
	}

	dnsNames := security.GetDNSNames(opts.Namespace)
	dnsNames = append(dnsNames, certgen.ParseCommaSeparatedString(opts.DNSNames)...)

	caCert, err := security.NewSelfSignedCACertificate()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cert, err := security.NewCertificateWithIPs(caCert, opts.CommonName, dnsNames, ipAddresses)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	secret := security.GenSecret(opts.SecretName, opts.Namespace, cert, caCert)

	// Build final YAML-safe struct
	output := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]string{
			"name":      secret.Name,
			"namespace": secret.Namespace,
		},
		"type": string(secret.Type),
		"data": encodeSecretData(secret),
	}

	yamlOut, err := yaml.Marshal(output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal yaml: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(yamlOut))
}
