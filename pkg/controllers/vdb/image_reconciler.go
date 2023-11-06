/*
 (c) Copyright [2021-2023] Open Text.
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
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const admintoolsBin = "/opt/vertica/bin/admintools"

// ImageCheckReconciler verifies that the operator use correct images for vcluster or admintools
// log events if we a wrong image is selected
type ImageCheckReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

// MakeImageCheckReconciler will build a ImageCheckReconciler object
func MakeImageCheckReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &ImageCheckReconciler{
		VRec:    vdbrecon,
		Log:     log.WithName("ImageCheckReconciler"),
		Vdb:     vdb,
		PRunner: prunner,
		PFacts:  pfacts,
	}
}

// Reconcile will look a running pod to check if admintools binary exists in the image.
func (c *ImageCheckReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	pf, found := c.PFacts.findRunningPod()
	if !found {
		c.Log.Info("No pods running") // No running pod.  This isn't an error, it just means no vertica is running
		return ctrl.Result{}, nil
	}
	stdout, _, _ := c.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer,
		"bash", "-c", "which admintools",
	)
	res := strings.TrimSpace(stdout) // remove leading and trailing whitespaces
	if vmeta.UseVClusterOps(c.Vdb.Annotations) {
		if res == admintoolsBin {
			c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.WrongImage, "Failed to choose an image suited for vclusterops")
			return ctrl.Result{}, fmt.Errorf("Failed to choose an image suited: the image %s is an admintools style of deployment "+
				",cannot be used for vcluster", c.Vdb.Spec.Image)
		}
	} else {
		if res == "" {
			c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.WrongImage, "Failed to choose an image suited for admintools")
			return ctrl.Result{}, fmt.Errorf("Failed to choose an image suited: the image %s is a vcluster style of deployment "+
				",cannot be used for admintools", c.Vdb.Spec.Image)
		}
	}

	return ctrl.Result{}, nil
}
