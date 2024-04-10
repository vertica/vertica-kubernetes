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

package sandbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VerifyDeploymentReconciler will verify the current deployment supports
// sandboxing with vclusterops
type VerifyDeploymentReconciler struct {
	SRec      *SandboxConfigMapReconciler
	ConfigMap *corev1.ConfigMap
	Vdb       *v1.VerticaDB
	Log       logr.Logger
}

func MakeVerifyDeploymentReconciler(r *SandboxConfigMapReconciler, cm *corev1.ConfigMap,
	vdb *v1.VerticaDB, log logr.Logger) controllers.ReconcileActor {
	return &VerifyDeploymentReconciler{
		SRec:      r,
		ConfigMap: cm,
		Log:       log.WithName("VerifyDeploymentReconciler"),
		Vdb:       vdb,
	}
}

// Reconcile will verify the current deployment supports
// sandboxing with vclusterops
func (s *VerifyDeploymentReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, s.checkDeployment()
}

// checkDeployment checks if version supports sandboxing with vclusterops
func (s *VerifyDeploymentReconciler) checkDeployment() error {
	var msg string
	if !vmeta.UseVClusterOps(s.Vdb.Annotations) {
		msg = fmt.Sprintf("the VerticaDB named %q has vclusterops disabled", s.ConfigMap.Data[verticaDBNameKey])
		s.Log.Info(msg)
		return errors.New(msg)
	}
	// we want the sandboxed cluster version that is why we get the version
	// the sandbox configmap
	vdbVer := s.ConfigMap.Annotations[vmeta.VersionAnnotation]
	vinf, err := version.MakeInfoFromStrCheck(vdbVer)
	if err != nil {
		return err
	}
	if !vinf.IsEqualOrNewer(v1.SandboxSupportedMinVersion) {
		msg = fmt.Sprintf("version %q does not support sandboxing with vclusterops",
			vdbVer)
		s.Log.Info(msg)
		return errors.New(msg)
	}
	return nil
}
