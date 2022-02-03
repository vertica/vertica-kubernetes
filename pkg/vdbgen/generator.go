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

package vdbgen

import (
	"fmt"
	"io"

	"github.com/ghodss/yaml"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

type KObjs struct {
	Vdb                     vapi.VerticaDB
	CredSecret              corev1.Secret
	HasLicense              bool
	LicenseSecret           corev1.Secret
	SuperuserPasswordSecret corev1.Secret
	HasPassword             bool
	HasCAFile               bool
	CAFile                  corev1.Secret
	HasHadoopConfig         bool
	HadoopConfig            corev1.ConfigMap
	HasKerberosSecret       bool
	KerberosSecret          corev1.Secret
}

type VDBCreator interface {
	Create() (*KObjs, error)
}

// Generate will construct the VerticaDB and print it to wr
func Generate(wr io.Writer, cr VDBCreator) error {
	objs, err := cr.Create()
	if err != nil {
		return err
	}

	return writeManifest(wr, objs)
}

// writeManifest will print out the objects in pretty-print yaml output
func writeManifest(wr io.Writer, objs *KObjs) (err error) {
	y, err := yaml.Marshal(objs.CredSecret)
	if err != nil {
		return err
	}
	fmt.Fprint(wr, string(y))

	if objs.HasPassword {
		y, err = yaml.Marshal(objs.SuperuserPasswordSecret)
		if err != nil {
			return err
		}
		fmt.Fprint(wr, "---\n")
		fmt.Fprint(wr, string(y))
	}

	if objs.HasLicense {
		y, err = yaml.Marshal(objs.LicenseSecret)
		if err != nil {
			return err
		}
		fmt.Fprint(wr, "---\n")
		fmt.Fprint(wr, string(y))
	}

	if objs.HasCAFile {
		y, err = yaml.Marshal(objs.CAFile)
		if err != nil {
			return err
		}
		fmt.Fprint(wr, "---\n")
		fmt.Fprint(wr, string(y))
	}

	if objs.HasHadoopConfig {
		y, err = yaml.Marshal(objs.HadoopConfig)
		if err != nil {
			return err
		}
		fmt.Fprint(wr, "---\n")
		fmt.Fprint(wr, string(y))
	}

	if objs.HasKerberosSecret {
		y, err = yaml.Marshal(objs.KerberosSecret)
		if err != nil {
			return err
		}
		fmt.Fprint(wr, "---\n")
		fmt.Fprint(wr, string(y))
	}

	y, err = yaml.Marshal(objs.Vdb)
	if err != nil {
		return err
	}
	fmt.Fprint(wr, "---\n")
	fmt.Fprint(wr, string(y))
	return nil
}
