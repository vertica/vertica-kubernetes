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

package vadmin

import (
	"context"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/saverestorepoint"
)

// SaveRestorePoint will create an archive point in the database
func (v *VClusterOps) SaveRestorePoint(ctx context.Context, opts ...saverestorepoint.Option) error {
	v.setupForAPICall("SaveRestorePoint")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster SaveRestorePoint")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return err
	}

	s := saverestorepoint.Params{}
	s.Make(opts...)

	// call vclusterOps library to sandbox a subcluster
	vopts := v.genSaveRestorePointOptions(&s, certs)
	err = v.VSaveRestorePoint(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to create a restore point to archive", "archive name",
			vopts.ArchiveName, "sandbox", vopts.Sandbox)
		return err
	}

	v.Log.Info("Successfully create a restore point to archive", "archive name",
		vopts.ArchiveName, "sandbox", vopts.Sandbox)
	return nil
}

func (v *VClusterOps) genSaveRestorePointOptions(s *saverestorepoint.Params, certs *HTTPSCerts) vops.VSaveRestorePointOptions {
	opts := vops.VSaveRestorePointFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.Hosts = []string{s.InitiatorIP}
	opts.ArchiveName = s.ArchiveName
	opts.Sandbox = s.Sandbox

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	return opts
}
