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

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/setconfigparameter"
)

// SetConfigurationParameter can set a given configuration parameter to a specified value
// at a certain value in the database
func (v *VClusterOps) SetConfigurationParameter(ctx context.Context, opts ...setconfigparameter.Option) (err error) {
	v.setupForAPICall("SetConfigurationParameter")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster SetConfigurationParameter")

	// certs, err := v.retrieveNMACerts(ctx)
	// if err != nil {
	// 	return err
	// }

	// s := setconfigparameter.Parms{}
	// s.Make(opts...)

	// vcOpts := v.genSetConfigurationParameterOptions(&s, certs)
	// err := v.VSetConfigurationParameters(vcOpts)
	// if err != nil {
	// 	return fmt.Errorf("failed to set configuration parameter: %w", err)
	// }

	return nil
}

// func (v *VClusterOps) genSetConfigurationParameterOptions(s *setconfigparameter.Parms, certs *HTTPSCerts) *vops.VSetConfigurationParameterOptions {
// 	opts := vops.VSetConfigurationParameterOptionsFactory()

// 	return &opts
// }
