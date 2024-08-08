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

package catalog

import (
	"context"

	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/getconfigparameter"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/setconfigparameter"
)

// GetConfigurationParameter returns the value of a config parameter from the given sandbox
func (v *VCluster) GetConfigurationParameter(param, level, sandbox string, ctx context.Context) (value string, err error) {
	vclusterOps := vadmin.MakeVClusterOps(v.Log, v.VDB, v.Client, v.Password, v.EVRec, vadmin.SetupVClusterOps)
	opts := []getconfigparameter.Option{
		getconfigparameter.WithUserName(v.VDB.GetVerticaUser()),
		getconfigparameter.WithInitiatorIP(v.PodIP),
		getconfigparameter.WithSandbox(sandbox),
		getconfigparameter.WithConfigParameter(param),
		getconfigparameter.WithLevel(level),
	}
	return vclusterOps.GetConfigurationParameter(ctx, opts...)
}

// SetConfigurationParameter sets the value of a configuration parameter in the given san
func (v *VCluster) SetConfigurationParameter(param, value, level, sandbox string, ctx context.Context) error {
	vclusterOps := vadmin.MakeVClusterOps(v.Log, v.VDB, v.Client, v.Password, v.EVRec, vadmin.SetupVClusterOps)
	opts := []setconfigparameter.Option{
		setconfigparameter.WithUserName(v.VDB.GetVerticaUser()),
		setconfigparameter.WithInitiatorIP(v.PodIP),
		setconfigparameter.WithSandbox(sandbox),
		setconfigparameter.WithConfigParameter(param),
		setconfigparameter.WithValue(value),
		setconfigparameter.WithLevel(level),
	}
	return vclusterOps.SetConfigurationParameter(ctx, opts...)
}
