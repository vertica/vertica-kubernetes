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

package vdb

import (
	"context"
	"fmt"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// distributeAdmintoolsConf will copy admintools.conf to all of the running
// pods.  It will target the base pod first since that is what will be used as
// the basis for admintools.conf in subsequent iterations.  If copying to the
// base pod is successful, then it will attempt to copy to all of the other pods
// -- checking errors at the end to ensure we attempt at each pod.
func distributeAdmintoolsConf(ctx context.Context, vdb *vapi.VerticaDB, vrec *VerticaDBReconciler,
	pf *PodFacts, pr cmds.PodRunner, atConfTempFile string) error {
	// We always distribute to a well known base pod first. The admintools.conf
	// on this pod is used as the base for any subsequent changes.
	basePod, err := findPodForFirstCopy(vdb, pf)
	if err != nil {
		return err
	}
	_, _, err = pr.CopyToPod(ctx, basePod, names.ServerContainer, atConfTempFile, paths.AdminToolsConf)
	if err != nil {
		return err
	}

	// Copy the admintools.conf to the rest of the pods.  We will do error
	// checking at the end so that we try to copy it to each pod.
	errs := []error{}
	for _, p := range pf.Detail {
		if !p.isPodRunning {
			continue
		}
		// Skip base pod as it was copied at the start of this function.
		if p.name == basePod {
			continue
		}
		_, _, e := pr.CopyToPod(ctx, p.name, names.ServerContainer, atConfTempFile, paths.AdminToolsConf)
		// Save off any error and go onto the next pod
		if e != nil {
			errs = append(errs, e)
		}
	}
	// If at least one error occurred, log an event and return the first error.
	if len(errs) > 0 {
		vrec.EVRec.Eventf(vdb, corev1.EventTypeWarning, events.ATConfPartiallyCopied,
			"Distributing new admintools.conf was successful only at some of the pods.  "+
				"There was an error copying to %d of the pod(s).", len(errs))
		return errs[0]
	}
	return nil
}

// findATBasePod will return the pod to use for the base admintools.conf file.
// The base is used as the initial state of admintools.conf.  The caller then
// applies any addition or removals of hosts from that base.
func findATBasePod(vdb *vapi.VerticaDB, pf *PodFacts) (types.NamespacedName, error) {
	// We always use pod -0 from the first subcluster as the base for the
	// admintools.conf.  We assume that all pods are running by the time we get
	// here.
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		pn := names.GenPodName(vdb, sc, 0)
		if pf.Detail[pn].isInstalled.IsTrue() {
			return pn, nil
		}
	}
	return types.NamespacedName{}, fmt.Errorf("couldn't find a suitable pod to install from")
}

// findPodForFirstCopy will pick the first pod that admintools.conf should be copied to.
func findPodForFirstCopy(vdb *vapi.VerticaDB, pf *PodFacts) (types.NamespacedName, error) {
	basePod, _ := findATBasePod(vdb, pf)
	if basePod != (types.NamespacedName{}) {
		return basePod, nil
	}
	// We get here if nothing has been installed yet.  We simply pick the first
	// pod that is running.
	for i := range vdb.Spec.Subclusters {
		sc := &vdb.Spec.Subclusters[i]
		pn := names.GenPodName(vdb, sc, 0)
		if pf.Detail[pn].isPodRunning {
			return pn, nil
		}
	}
	return types.NamespacedName{}, fmt.Errorf("couldn't find a suitable pod to install from")
}
