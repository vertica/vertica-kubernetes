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

package paths

const (
	// A file to denote the /config dir has been setup.  Note, we don't call
	// update_vertica anymore, but it is kept in the name for backwards
	// compatibility.
	InstallerIndicatorFile = "/opt/vertica/config/update_vertica.called.for.uid."
	LocalDataPath          = "/home/dbadmin/local-data"
	CELicensePath          = "/home/dbadmin/licensing/ce/vertica_community_edition.license.key"
	MountedLicensePath     = "/home/dbadmin/licensing/mnt"
	HadoopConfPath         = "/etc/hadoop"
	ConfigPath             = "/opt/vertica/config"
	ConfigSharePath        = "/opt/vertica/config/share"
	ConfigLogrotatePath    = "/opt/vertica/config/logrotate"
	HTTPTLSConfDir         = "/opt/vertica/config/https_certs"
	HTTPTLSConfFile        = "httpstls.json"
	LogPath                = "/opt/vertica/log"
	PodInfoPath            = "/etc/podinfo"
	AdminToolsConf         = "/opt/vertica/config/admintools.conf"
	AuthParmsFile          = "/home/dbadmin/auth_parms.conf"
	EulaAcceptanceFile     = "/opt/vertica/config/d5415f948449e9d4c421b568f2411140.dat"
	EulaAcceptanceScript   = "/opt/vertica/config/accept_eula.py"
	CertsRoot              = "/certs"
	HTTPServerCertsRoot    = "/certs/http-server"
	Krb5Conf               = "/etc/krb5.conf"
	Krb5Keytab             = "/etc/krb5/krb5.keytab"
	SSHPath                = "/home/dbadmin/.ssh"
	HTTPServerCACrtName    = "ca.crt"
)

// MountPaths lists all of the paths for internally generated mounts.
var MountPaths = []string{LocalDataPath, CELicensePath, MountedLicensePath,
	HadoopConfPath, ConfigPath, ConfigSharePath, ConfigLogrotatePath,
	LogPath, PodInfoPath, AdminToolsConf, AuthParmsFile, EulaAcceptanceFile,
	EulaAcceptanceScript, CertsRoot, Krb5Conf, Krb5Keytab, SSHPath}

// SSHKeyPaths is a list of keys that must exist in the SSHSecret
var SSHKeyPaths = []string{"id_rsa", "id_rsa.pub", "authorized_keys"}
