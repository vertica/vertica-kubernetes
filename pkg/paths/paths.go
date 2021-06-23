/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

import (
	"fmt"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

const (
	InstallerIndicatorFile = "/opt/vertica/config/update_vertica.called.for.uid."
	LocalDataPath          = "/home/dbadmin/local-data"
	CELicensePath          = "/home/dbadmin/licensing/ce/vertica_community_edition.license.key"
	MountedLicensePath     = "/home/dbadmin/licensing/mnt"
	ConfigPath             = "/opt/vertica/config"
	LogPath                = "/opt/vertica/log"
	PodInfoPath            = "/etc/podinfo"
	AdminToolsConf         = "/opt/vertica/config/admintools.conf"
	AuthParmsFile          = "/home/dbadmin/auth_parms.conf"
)

// GenInstallerIndicatorFileName returns the name of the installer indicator file.
// Valid only for the current instance of the vdb.
func GenInstallerIndicatorFileName(vdb *vapi.VerticaDB) string {
	return InstallerIndicatorFile + string(vdb.UID)
}

// GetPVSubPath returns the subpath in the local data PV.
// We use the UID so that we create unique paths in the PV.  If the PV is reused
// for a new vdb, the UID will be different.
func GetPVSubPath(vdb *vapi.VerticaDB, subPath string) string {
	return fmt.Sprintf("%s/%s", vdb.UID, subPath)
}

// GetDBDataPath get the data path for the current database
func GetDBDataPath(vdb *vapi.VerticaDB) string {
	return fmt.Sprintf("%s/%s", vdb.Spec.Local.DataPath, vdb.Spec.DBName)
}

// GetCommunalPath returns the path to use for communal storage
func GetCommunalPath(vdb *vapi.VerticaDB) string {
	// We include the UID in the communal path to generate a unique path for
	// each new instance of vdb. This means we can't use the same base path for
	// different databases and we don't require any cleanup if the vdb was
	// recreated.
	if !vdb.Spec.Communal.IncludeUIDInPath {
		return vdb.Spec.Communal.Path
	}
	return fmt.Sprintf("%s/%s", vdb.Spec.Communal.Path, vdb.UID)
}

func GetDepotPath(vdb *vapi.VerticaDB) string {
	return fmt.Sprintf("%s/%s", vdb.Spec.Local.DepotPath, vdb.Spec.DBName)
}
