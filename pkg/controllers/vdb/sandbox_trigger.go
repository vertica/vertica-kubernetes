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

	"github.com/google/uuid"
	"github.com/pkg/errors"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
)

// SandboxTrigger triggers the sandbox controller
// for a given sandbox
type SandboxTrigger struct {
	vrec      config.ReconcilerInterface
	vdb       *vapi.VerticaDB
	configMap *corev1.ConfigMap
	sandbox   string
	triggerID string
}

func MakeSandboxTrigger(recon config.ReconcilerInterface, vdb *vapi.VerticaDB,
	sbName, id string) *SandboxTrigger {
	return &SandboxTrigger{
		vrec:      recon,
		vdb:       vdb,
		sandbox:   sbName,
		triggerID: id,
		configMap: &corev1.ConfigMap{},
	}
}

// triggerSandboxController will wake up the sandbox controller by setting
// the vdb resource version in the sandbox configmap annotations
func (s *SandboxTrigger) triggerSandboxController(ctx context.Context) (bool, error) {
	if err := s.fetchConfigMap(ctx); err != nil {
		return false, err
	}
	triggerID := s.getTriggerID()
	anns := map[string]string{
		// This will ensure that we always set a new value
		// for this annotation which will wake up the sandbox
		// controller
		vmeta.SandboxControllerTriggerID: triggerID,
	}
	chgs := vk8s.MetaChanges{
		NewAnnotations: anns,
	}
	nm := names.GenConfigMapName(s.vdb, s.sandbox)
	return vk8s.MetaUpdate(ctx, s.vrec.GetClient(), nm, s.configMap, chgs)
}

// fetchConfigMap will fetch the sandbox configmap
func (s *SandboxTrigger) fetchConfigMap(ctx context.Context) error {
	nm := names.GenConfigMapName(s.vdb, s.sandbox)
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
func (s *SandboxTrigger) validateConfigMapDataValues() bool {
	vdbName := s.configMap.Data[vapi.VerticaDBNameKey]
	sbName := s.configMap.Data[vapi.SandboxNameKey]
	return vdbName == s.vdb.Name && sbName == s.sandbox
}

func (s *SandboxTrigger) getTriggerID() string {
	if s.triggerID != "" {
		return s.triggerID
	}
	// Let's generate an id if one is not set
	return uuid.NewString()
}
