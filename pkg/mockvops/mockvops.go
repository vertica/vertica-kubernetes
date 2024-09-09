/*
Copyright [2021-2024] Open Text.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mockvops

import (
	"github.com/go-logr/logr"
	"github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MockVClusterOps is used to invoke mock vcluster-ops functions;
// MockVClusterOps implements vadmin.VClusterProvider interface;
// MockVClusterOps returns zero values for all return types for
// all mock API calls; to override behavior behavior
// of specific mock API calls create a struct that embeds MockVClusterOps
// and provides implementation for functions to override
type MockVClusterOps struct{}

func (*MockVClusterOps) VAddNode(_ *vclusterops.VAddNodeOptions) (vclusterops.VCoordinationDatabase, error) {
	return vclusterops.VCoordinationDatabase{}, nil
}
func (*MockVClusterOps) VAddSubcluster(_ *vclusterops.VAddSubclusterOptions) error {
	return nil
}
func (*MockVClusterOps) VCreateDatabase(_ *vclusterops.VCreateDatabaseOptions) (vclusterops.VCoordinationDatabase, error) {
	return vclusterops.VCoordinationDatabase{}, nil
}
func (*MockVClusterOps) VFetchNodeState(_ *vclusterops.VFetchNodeStateOptions) ([]vclusterops.NodeInfo, error) {
	return nil, nil
}
func (*MockVClusterOps) VReIP(_ *vclusterops.VReIPOptions) error {
	return nil
}
func (*MockVClusterOps) VRemoveNode(_ *vclusterops.VRemoveNodeOptions) (vclusterops.VCoordinationDatabase, error) {
	return vclusterops.VCoordinationDatabase{}, nil
}
func (*MockVClusterOps) VRemoveSubcluster(_ *vclusterops.VRemoveScOptions) (vclusterops.VCoordinationDatabase, error) {
	return vclusterops.VCoordinationDatabase{}, nil
}
func (*MockVClusterOps) VReviveDatabase(_ *vclusterops.VReviveDatabaseOptions) (string, *vclusterops.VCoordinationDatabase, error) {
	return "", nil, nil
}
func (*MockVClusterOps) VShowRestorePoints(_ *vclusterops.VShowRestorePointsOptions) ([]vclusterops.RestorePoint, error) {
	return nil, nil
}
func (*MockVClusterOps) VStartDatabase(_ *vclusterops.VStartDatabaseOptions) (*vclusterops.VCoordinationDatabase, error) {
	return nil, nil
}
func (*MockVClusterOps) VStartNodes(_ *vclusterops.VStartNodesOptions) error {
	return nil
}
func (*MockVClusterOps) VStopDatabase(_ *vclusterops.VStopDatabaseOptions) error {
	return nil
}
func (*MockVClusterOps) VInstallPackages(_ *vclusterops.VInstallPackagesOptions) (*vclusterops.InstallPackageStatus, error) {
	return nil, nil
}
func (*MockVClusterOps) VReplicateDatabase(_ *vclusterops.VReplicationDatabaseOptions) error {
	return nil
}
func (*MockVClusterOps) VFetchNodesDetails(_ *vclusterops.VFetchNodesDetailsOptions) (vclusterops.NodesDetails, error) {
	return nil, nil
}
func (*MockVClusterOps) VSandbox(_ *vclusterops.VSandboxOptions) error {
	return nil
}
func (*MockVClusterOps) VUnsandbox(_ *vclusterops.VUnsandboxOptions) error {
	return nil
}
func (*MockVClusterOps) VCreateArchive(_ *vclusterops.VCreateArchiveOptions) error {
	return nil
}
func (*MockVClusterOps) VPromoteSandboxToMain(_ *vclusterops.VPromoteSandboxToMainOptions) error {
	return nil
}
func (m *MockVClusterOps) VAlterSubclusterType(options *vclusterops.VAlterSubclusterTypeOptions) error {
	return nil
}
func (m *MockVClusterOps) VSetConfigurationParameters(options *vclusterops.VSetConfigurationParameterOptions) error {
	return nil
}
func (m *MockVClusterOps) VGetConfigurationParameters(options *vclusterops.VGetConfigurationParameterOptions) (string, error) {
	return "", nil
}
func (*MockVClusterOps) VRenameSubcluster(_ *vclusterops.VRenameSubclusterOptions) error {
	return nil
}
func (m *MockVClusterOps) VPollSubclusterState(_ *vclusterops.VPollSubclusterStateOptions) error {
	return nil
}

// MakeMockVClusterOpsDispatch will create a mock vcluster dispatcher
func MakeMockVClusterOpsDispatcher(vdb *vapi.VerticaDB, logger logr.Logger, cl client.Client,
	setupAPIFunc func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger)) *vadmin.VClusterOps {
	evWriter := aterrors.TestEVWriter{}
	const testPassword = "secret"
	dispatcher := vadmin.MakeVClusterOps(logger, vdb, cl, testPassword, &evWriter, setupAPIFunc)
	return dispatcher.(*vadmin.VClusterOps)
}
