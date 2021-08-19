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

package controllers

import (
	"context"
	"fmt"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"k8s.io/apimachinery/pkg/types"
)

// distributeAdmintoolsConf will copy an admintools.conf to all of the running pods
func distributeAdmintoolsConf(ctx context.Context, pf *PodFacts, pr cmds.PodRunner, atConfTempFile string) error {
	for _, p := range pf.Detail {
		if !p.isPodRunning {
			continue
		}
		_, _, err := pr.CopyToPod(ctx, p.name, names.ServerContainer, atConfTempFile, paths.AdminToolsConf)
		if err != nil {
			return err
		}
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
