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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createarchive"
)

// CreateArchive will create an archive point in the database
func (v *VClusterOps) CreateArchive(ctx context.Context, opts ...createarchive.Option) error {
	v.setupForAPICall("CreateArchive")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster CreateArchive")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return err
	}

	s := createarchive.Params{}
	s.Make(opts...)

	// call vclusterOps library to sandbox a subcluster
	vopts := v.genCreateArchiveOptions(&s, certs)
	err = v.VCreateArchive(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to add a create an archive", "archive name",
			vopts.ArchiveName, "sandbox", vopts.Sandbox, "num restore point", vopts.NumOfArchives)
		return err
	}

	v.Log.Info("Successfully create an archive", "archive name",
		vopts.ArchiveName, "sandbox", vopts.Sandbox, "num restore point", vopts.NumOfArchives)
	return nil
}

func (v *VClusterOps) genCreateArchiveOptions(s *createarchive.Params, certs *HTTPSCerts) vops.VCreateArchiveOptions {
	opts := vops.VCreateArchiveFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	// DEBUG method
	v.Log.Info("Setup create archive options", "archive name", s.ArchiveName)
	v.Log.Info("Setup create archive options", "sandbox", s.Sandbox)
	v.Log.Info("Setup create archive options", "num restore point", s.NumOfArchives)

	opts.ArchiveName = s.ArchiveName
	opts.Sandbox = s.Sandbox
	opts.NumOfArchives = s.NumOfArchives

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	return opts
}
