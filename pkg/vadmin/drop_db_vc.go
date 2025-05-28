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
	"path/filepath"

	vops "github.com/vertica/vcluster/vclusterops"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/dropdb"
)

// DropDB will drop vertica.conf and catalog files before db revival
func (v *VClusterOps) DropDB(ctx context.Context, opts ...dropdb.Option) error {
	v.setupForAPICall("DropDB")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster DropDB")

	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return err
	}

	s := dropdb.Parms{}
	s.Make(opts...)

	vcOpts := v.genDropDBOptions(&s, certs)
	err = v.VDropDatabase(vcOpts)
	if err != nil {
		return err
	}

	v.Log.Info("Successfully dropped the database", "database", vcOpts.DBName)
	return nil
}

func (v *VClusterOps) genDropDBOptions(s *dropdb.Parms, certs *HTTPSCerts) *vops.VDropDatabaseOptions {
	opts := vops.VDropDatabaseOptionsFactory()

	opts.DBName = s.DBName
	for _, h := range s.Hosts {
		var node vops.VCoordinationNode
		node.Name = h.VNode
		node.Address = h.IP
		pathSuffix := h.VNode + "_catalog"
		node.CatalogPath = filepath.Join(v.VDB.Spec.Local.GetCatalogPath(), s.DBName, pathSuffix)
		opts.NodesToDrop = append(opts.NodesToDrop, node)
	}

	if len(opts.NodesToDrop) > 0 {
		opts.IPv6 = net.IsIPv6(opts.NodesToDrop[0].Address)
	}
	opts.RetainCatalogDir = vmeta.GetPreserveDBDirectory(v.VDB.Annotations)
	v.Log.Info("Setup drop database options", "opts", opts)

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	return &opts
}
