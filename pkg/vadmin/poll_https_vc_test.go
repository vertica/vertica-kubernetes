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
	. "github.com/onsi/ginkgo/v2"
	vops "github.com/vertica/vcluster/vclusterops"
)

// mock version of VPollHTTPS() that is invoked inside VClusterOps.VPollHTTPS()
func (m *MockVClusterOps) VPollHTTPS(options *vops.VPollHTTPSOptions) (err error) {
	return nil
}

var _ = Describe("set_config_parameter_vc", func() {

})
