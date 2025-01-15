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

package util

// this file defines basic default values
const (
	DefaultClientPort                = 5433
	DefaultHTTPPortOffset            = 3010
	DefaultHTTPPort                  = DefaultClientPort + DefaultHTTPPortOffset
	DefaultControlAddressFamily      = "ipv4"
	IPv6ControlAddressFamily         = "ipv6"
	DefaultRestartPolicy             = "ksafe"
	DefaultDBDir                     = "/opt/vertica"
	DefaultShareDir                  = DefaultDBDir + "/share"
	DefaultLicenseKey                = DefaultShareDir + "/license.key"
	DefaultConfigDir                 = DefaultDBDir + "/config"
	DefaultRetryCount                = 3
	DefaultTimeoutSeconds            = 300
	DefaultLoadCatalogTimeoutSeconds = 3600
	DefaultStatePollingTimeout       = 1200
	DefaultLargeCluster              = -1
	DefaultP2p                       = true
	DefaultSpreadLoggingLevel        = -1
	MaxLargeCluster                  = 120
	MinDepotSize                     = 0
	MaxDepotSize                     = 100
	DefaultDrainSeconds              = 60
	DefaultControlSetSize            = -1
	NodeUpState                      = "UP"
	NodeDownState                    = "DOWN"
	NodeComputeState                 = "COMPUTE"
	NodeUnknownState                 = "UNKNOWN" // this is for sandbox only
	SuppressHelp                     = "SUPPRESS_HELP"
	MainClusterSandbox               = ""
)

// TLS authentication related consts
const (
	// IPv4 defaults
	DefaultIPv4AuthName  = "vcluster_mtls_v4"
	DefaultIPv4AuthHosts = "0.0.0.0/0"
	// IPv6 defaults
	DefaultIPv6AuthName  = "vcluster_mtls_v6"
	DefaultIPv6AuthHosts = "::/0"
)

var RestartPolicyList = []string{"never", DefaultRestartPolicy, "always"}
