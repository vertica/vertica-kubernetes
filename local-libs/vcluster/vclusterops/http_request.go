/*
 (c) Copyright [2023-2024] Open Text.
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

package vclusterops

type hostHTTPRequest struct {
	Method       string
	Endpoint     string
	IsNMACommand bool
	QueryParams  map[string]string
	RequestData  string // the data must be a JSON-encoded string
	Username     string // optional, for HTTPS endpoints only
	// string pointer is used here as we need to check whether the password has been set
	Password *string // optional, for HTTPS endpoints only
	Timeout  int     // optional, set it if an Op needs longer time to complete

	// optional, for calling NMA/Vertica HTTPS endpoints. If Username/Password is set, that takes precedence over this for HTTPS calls.
	UseCertsInOptions   bool
	Certs               httpsCerts
	TLSDoVerify         bool
	TLSDoVerifyHostname bool
}

type httpsCerts struct {
	key    string
	cert   string
	caCert string
}

type tlsModes struct {
	doVerifyNMAServerCert    bool
	doVerifyHTTPSServerCert  bool
	doVerifyPeerCertHostname bool
}

func (req *hostHTTPRequest) setCerts(certs *httpsCerts) {
	if certs == nil {
		return
	}
	req.UseCertsInOptions = true
	req.Certs.key = certs.key
	req.Certs.cert = certs.cert
	req.Certs.caCert = certs.caCert
}

func (req *hostHTTPRequest) setTLSMode(modes *tlsModes) {
	if modes == nil {
		return
	}
	if req.IsNMACommand {
		req.TLSDoVerify = modes.doVerifyNMAServerCert
	} else {
		req.TLSDoVerify = modes.doVerifyHTTPSServerCert
	}
	// only do hostname validation if regular validation is enabled
	if req.TLSDoVerify {
		req.TLSDoVerifyHostname = modes.doVerifyPeerCertHostname
	}
}

func (req *hostHTTPRequest) buildNMAEndpoint(url string) {
	req.IsNMACommand = true
	req.Endpoint = NMACurVersion + url
}

func (req *hostHTTPRequest) buildHTTPSEndpoint(url string) {
	req.IsNMACommand = false
	req.Endpoint = HTTPCurVersion + url
}

// this is used as the "ATModuleBase" in Admintools
type clusterHTTPRequest struct {
	RequestCollection map[string]hostHTTPRequest
	ResultCollection  map[string]hostHTTPResult
	SemVar            semVer
	Name              string
}
