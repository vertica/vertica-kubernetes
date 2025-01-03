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

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/util"
)

func TestGetDescriptionFilePath(t *testing.T) {
	opt := VReviveDBOptionsFactory()
	opt.DBName = "test_eon_db"

	// local communal storage:
	targetPath := "/communal/metadata/test_eon_db/cluster_config.json"
	// case 1: normal communal storage path
	opt.CommunalStorageLocation = "/communal"
	path := opt.getCurrConfigFilePath(util.MainClusterSandbox)
	assert.Equal(t, targetPath, path)

	// case 2: double-slash communal storage path
	opt.CommunalStorageLocation = "//communal"
	path = opt.getCurrConfigFilePath(util.MainClusterSandbox)
	assert.Equal(t, targetPath, path)

	// case 3: double-slash communal storage path followed by a slash
	opt.CommunalStorageLocation = "//communal/"
	path = opt.getCurrConfigFilePath(util.MainClusterSandbox)
	assert.Equal(t, targetPath, path)

	// case 4: double-slash communal storage path followed by a double-slash
	opt.CommunalStorageLocation = "//communal//"
	path = opt.getCurrConfigFilePath(util.MainClusterSandbox)
	assert.Equal(t, targetPath, path)

	// remote communal storage:
	targetS3Path := "s3://vertica-fleeting/k8s/revive_eon_5/metadata/test_eon_db/cluster_config.json"
	targetGCPPath := "gs://vertica-fleeting/k8s/revive_eon_5/metadata/test_eon_db/cluster_config.json"
	// case 1 - normal s3 communal storage:
	opt.CommunalStorageLocation = "s3://vertica-fleeting/k8s/revive_eon_5"
	path = opt.getCurrConfigFilePath(util.MainClusterSandbox)
	assert.Equal(t, targetS3Path, path)

	// case 2: double-slash s3 communal storage path
	opt.CommunalStorageLocation = "s3://vertica-fleeting//k8s//revive_eon_5"
	path = opt.getCurrConfigFilePath(util.MainClusterSandbox)
	assert.Equal(t, targetS3Path, path)

	// case 3: other cloud communal storage paths like GCP
	opt.CommunalStorageLocation = "gs://vertica-fleeting/k8s/revive_eon_5"
	path = opt.getCurrConfigFilePath(util.MainClusterSandbox)
	assert.Equal(t, targetGCPPath, path)
}
