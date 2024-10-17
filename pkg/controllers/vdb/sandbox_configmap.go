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

package vdb

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
)

// SandboxConfigMapManager allows to make some actions
// on a sandbox's configmap
type SandboxConfigMapManager struct {
	vrec      config.ReconcilerInterface
	vdb       *vapi.VerticaDB
	configMap *corev1.ConfigMap
	sandbox   string
	triggerID string
}

func MakeSandboxConfigMapManager(recon config.ReconcilerInterface, vdb *vapi.VerticaDB,
	sbName, id string) *SandboxConfigMapManager {
	return &SandboxConfigMapManager{
		vrec:      recon,
		vdb:       vdb,
		sandbox:   sbName,
		triggerID: id,
		configMap: &corev1.ConfigMap{},
	}
}

type TriggerPurpose int

const (
	SandboxUpgrade = iota
	Unsandbox
	Shutdown
)

// triggerSandboxController will wake up the sandbox controller by setting
// a uuid in the sandbox configmap annotations
func (s *SandboxConfigMapManager) triggerSandboxController(ctx context.Context, purpose TriggerPurpose) (bool, error) {
	if err := s.fetchConfigMap(ctx); err != nil {
		return false, err
	}
	triggerID := s.getTriggerID()
	anns := make(map[string]string)
	// This will ensure that we always set a new value
	// for this annotation which will wake up the sandbox
	// controller
	switch purpose {
	case SandboxUpgrade:
		anns[vmeta.SandboxControllerUpgradeTriggerID] = triggerID
	case Unsandbox:
		anns[vmeta.SandboxControllerUnsandboxTriggerID] = triggerID
	case Shutdown:
		anns[vmeta.SandboxControllerShutdownTriggerID] = triggerID
	}
	chgs := vk8s.MetaChanges{
		NewAnnotations: anns,
	}
	nm := names.GenSandboxConfigMapName(s.vdb, s.sandbox)
	return vk8s.MetaUpdate(ctx, s.vrec.GetClient(), nm, s.configMap, chgs)
}

// fetchConfigMap will fetch the sandbox configmap
func (s *SandboxConfigMapManager) fetchConfigMap(ctx context.Context) error {
	nm := names.GenSandboxConfigMapName(s.vdb, s.sandbox)
	err := s.vrec.GetClient().Get(ctx, nm, s.configMap)
	if err != nil {
		return err
	}
	ok := s.validateConfigMapDataValues()
	if !ok {
		return errors.Errorf("invalid configMap %s", nm.Name)
	}
	return nil
}

// validateConfigMap checks that the configMap contains valid fields
func (s *SandboxConfigMapManager) validateConfigMapDataValues() bool {
	vdbName := s.configMap.Data[vapi.VerticaDBNameKey]
	sbName := s.configMap.Data[vapi.SandboxNameKey]
	return vdbName == s.vdb.Name && sbName == s.sandbox
}

// getSandboxVersion returns the vertica version running in the sandbox
func (s *SandboxConfigMapManager) getSandboxVersion(ctx context.Context) (ver string, ok bool, err error) {
	err = s.fetchConfigMap(ctx)
	if err != nil {
		return "", false, err
	}
	ver, ok = s.configMap.Annotations[vmeta.VersionAnnotation]
	return ver, ok, nil
}

func (s *SandboxConfigMapManager) getTriggerID() string {
	if s.triggerID != "" {
		return s.triggerID
	}
	// Let's generate an id if one is not set
	return uuid.NewString()
}

func (s *SandboxConfigMapManager) deleteConfigMap(ctx context.Context) (bool, error) {
	err := s.fetchConfigMap(ctx)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed trying to fetch configmap: %w", err)
	}
	return true, s.vrec.GetClient().Delete(ctx, s.configMap)
}
