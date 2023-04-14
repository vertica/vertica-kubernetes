/*
 (c) Copyright [2021-2022] Open Text.
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

// HTTPSTLSConf is the tls config file contents.  The format of this must match
// the format that the Vertica server understands.  Do not add or change fields
// here without first having an approriate change in the Vertica server.
type HTTPSTLSConf struct {
	Name         string   `json:"name"`
	CipherSuites string   `json:"cipher_suites"`
	Mode         int      `json:"mode"`        // TLS modes. Number is an internal server const (see vertica/Util/TLS.hpp)
	Key          string   `json:"key"`         // Key is the private key
	Certificate  string   `json:"certificate"` // Certificate associated with the private key
	ChainCerts   []string `json:"chain_certs"`
	CACerts      []string `json:"ca_certificates"`
}
