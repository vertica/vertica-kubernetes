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

package iter

import (
	"context"
	"fmt"
	"sort"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SubclusterFinder struct {
	client.Client
	Vdb         *vapi.VerticaDB
	Subclusters map[string]*vapi.Subcluster
}

type FindFlags uint8

const (
	// Find subclusters that appear in the vdb.
	FindInVdb FindFlags = 1 << iota
	// Find subclusters that don't appear in the vdb.  This can be for
	// subclusters that are being deleted.
	FindNotInVdb
	// Find subclusters that currently exist.  This includes subclusters that
	// are already present in the vdb as well as ones that are scheduled for
	// deletion.  This option is mutually exclusive with the other options.
	FindExisting
	// Find will return a list of objects that are sorted by their name
	FindSorted
	// Find all subclusters, both in the vdb and not in the vdb.
	FindAll = FindInVdb | FindNotInVdb
)

func MakeSubclusterFinder(cli client.Client, vdb *vapi.VerticaDB) SubclusterFinder {
	return SubclusterFinder{
		Client:      cli,
		Vdb:         vdb,
		Subclusters: vdb.GenSubclusterMap(),
	}
}

// FindStatefulSets returns the statefulsets that were created by the operator.
// You can limit it so that it only returns statefulsets that match subclusters
// in Vdb, ones that don't match or all.
//
//nolint:dupl
func (m *SubclusterFinder) FindStatefulSets(ctx context.Context, flags FindFlags, sandbox string) (*appsv1.StatefulSetList, error) {
	sts := &appsv1.StatefulSetList{}
	if err := m.buildObjList(ctx, sts, flags); err != nil {
		return nil, err
	}
	// we can skip the filtering if all of the sts we are
	// looking for are not for a subcluster in vdb
	if flags&FindInVdb != 0 || flags&FindExisting != 0 {
		sts = m.filterStatefulsetsBySandbox(sts, sandbox)
	}
	if flags&FindSorted != 0 {
		sort.Slice(sts.Items, func(i, j int) bool {
			return sts.Items[i].Name < sts.Items[j].Name
		})
	}
	return sts, nil
}

// FindServices returns service objects that are in use for subclusters
func (m *SubclusterFinder) FindServices(ctx context.Context, flags FindFlags) (*corev1.ServiceList, error) {
	svcs := &corev1.ServiceList{}
	if err := m.buildObjList(ctx, svcs, flags); err != nil {
		return nil, err
	}
	if flags&FindSorted != 0 {
		sort.Slice(svcs.Items, func(i, j int) bool {
			return svcs.Items[i].Name < svcs.Items[j].Name
		})
	}
	return svcs, nil
}

// FindPods returns pod objects that are are used to run Vertica.  It limits the
// pods that were created by the VerticaDB object.
//
//nolint:dupl
func (m *SubclusterFinder) FindPods(ctx context.Context, flags FindFlags, sandbox string) (*corev1.PodList, error) {
	pods := &corev1.PodList{}
	if err := m.buildObjList(ctx, pods, flags); err != nil {
		return nil, err
	}
	// we can skip the filtering if all of the pods we are
	// looking for are not part of a subcluster in vdb
	if flags&FindInVdb != 0 || flags&FindExisting != 0 {
		pods = m.filterPodsBySandbox(pods, sandbox)
	}
	if flags&FindSorted != 0 {
		sort.Slice(pods.Items, func(i, j int) bool {
			return pods.Items[i].Name < pods.Items[j].Name
		})
	}
	return pods, nil
}

// FindSubclusters will return a list of subclusters.
// It accepts a flags field to indicate whether to return subclusters in the vdb,
// not in the vdb or both.
func (m *SubclusterFinder) FindSubclusters(ctx context.Context, flags FindFlags, sandbox string) ([]*vapi.Subcluster, error) {
	subclusters := []*vapi.Subcluster{}
	isMainCluster := sandbox == vapi.MainCluster

	if flags&FindInVdb != 0 {
		subclusters = append(subclusters, m.getVdbSubclusters(sandbox)...)
	}

	// a sandboxed subcluster can only be one of those in the vdb so we skip this part if
	// we are looking for sandboxed subclusters
	if isMainCluster && (flags&FindNotInVdb != 0 || flags&FindExisting != 0) {
		missingSts, err := m.FindStatefulSets(ctx, flags & ^FindInVdb, vapi.MainCluster)
		if err != nil {
			return nil, err
		}

		// We will convert each statefulset into a vapi.Subcluster stub object.  We
		// only fill in the name.  Size is intentionally left zero as this is an
		// indication the subcluster is being removed.
		for i := range missingSts.Items {
			scName := missingSts.Items[i].Labels[vmeta.SubclusterNameLabel]
			subclusters = append(subclusters, &vapi.Subcluster{Name: scName, Size: 0})
		}
	}

	if flags&FindSorted != 0 {
		sort.Slice(subclusters, func(i, j int) bool {
			return subclusters[i].Name < subclusters[j].Name
		})
	}
	return subclusters, nil
}

// hasSubclusterLabelFromVdb returns true if the given set of labels include a subcluster that is in the vdb
func (m *SubclusterFinder) hasSubclusterLabelFromVdb(objLabels map[string]string) bool {
	scName := objLabels[vmeta.SubclusterNameLabel]
	_, ok := m.Subclusters[scName]
	return ok
}

// buildObjList will populate list with an object type owned by the operator.
// Caller can use flags to return a list of all objects, only those in the vdb,
// or only those not in the vdb.
func (m *SubclusterFinder) buildObjList(ctx context.Context, list client.ObjectList, flags FindFlags) error {
	if err := listObjectsOwnedByOperator(ctx, m.Client, m.Vdb, list); err != nil {
		return err
	}
	if flags&FindAll == FindAll {
		return nil
	}
	rawObjs := []runtime.Object{}
	if err := meta.EachListItem(list, func(obj runtime.Object) error {
		l, ok := getLabelsFromObject(obj)
		if !ok {
			return fmt.Errorf("could not find labels from k8s object %s", obj)
		}
		// Skip if object is not subcluster specific.  This is necessary for objects like
		// the headless service object that is cluster wide.
		if !hasSubclusterNameLabel(l) {
			return nil
		}
		if flags&FindExisting != 0 {
			rawObjs = append(rawObjs, obj)
			return nil
		}
		isScFromVdb := m.hasSubclusterLabelFromVdb(l)
		if flags&FindInVdb != 0 && isScFromVdb {
			rawObjs = append(rawObjs, obj)
			return nil
		} else if flags&FindNotInVdb != 0 && !isScFromVdb {
			rawObjs = append(rawObjs, obj)
			return nil
		}
		return nil
	}); err != nil {
		return err
	}
	return meta.SetList(list, rawObjs)
}

// hasSubclusterNameLabel returns true if there exists a label that indicates
// the object is for a subcluster
func hasSubclusterNameLabel(l map[string]string) bool {
	_, ok := l[vmeta.SubclusterNameLabel]
	if ok {
		return true
	}
	// Prior to 1.3.0, we had a different name for the subcluster name.  We
	// renamed it as we added additional subcluster attributes to the label.
	// Check for this one too.
	_, ok = l[vmeta.SubclusterLegacyNameLabel]
	return ok
}

// getSubclusterName returns the subcluster name from the labels
func getSubclusterName(l map[string]string) string {
	sc, ok := l[vmeta.SubclusterNameLabel]
	if ok {
		return sc
	}
	// Prior to 1.3.0, we had a different name for the subcluster name.  We
	// renamed it as we added additional subcluster attributes to the label.
	// Check for this one too.
	return l[vmeta.SubclusterLegacyNameLabel]
}

// getLabelsFromObject will extract the labels from a k8s object.
// If labels were not found then false is return for bool output.
//
//nolint:gocritic
func getLabelsFromObject(obj runtime.Object) (map[string]string, bool) {
	if sts, ok := obj.(*appsv1.StatefulSet); ok {
		return sts.Labels, true
	} else if svc, ok := obj.(*corev1.Service); ok {
		return svc.Labels, true
	} else if pod, ok := obj.(*corev1.Pod); ok {
		return pod.Labels, true
	}
	return nil, false
}

// getSubclusterSandboxStatusMap returns a map that contains sandboxed
// subclusters
func (m *SubclusterFinder) getSubclusterSandboxStatusMap() map[string]string {
	scMap := map[string]string{}
	for i := range m.Vdb.Status.Sandboxes {
		sb := &m.Vdb.Status.Sandboxes[i]
		for _, sc := range sb.Subclusters {
			scMap[sc] = sb.Name
		}
	}
	return scMap
}

// getVdbSubclusters returns the subclusters that are in the vdb
func (m *SubclusterFinder) getVdbSubclusters(sandbox string) []*vapi.Subcluster {
	if sandbox != vapi.MainCluster {
		return m.getSandboxedSubclusters(sandbox)
	}
	return m.getMainSubclusters()
}

// getMainSubclusters returns the subclusters that are not part
// of any sandboxes
func (m *SubclusterFinder) getMainSubclusters() []*vapi.Subcluster {
	subclusters := []*vapi.Subcluster{}
	scMap := m.getSubclusterSandboxStatusMap()
	for i := range m.Vdb.Spec.Subclusters {
		sc := &m.Vdb.Spec.Subclusters[i]
		if _, ok := scMap[sc.Name]; !ok {
			subclusters = append(subclusters, sc)
		}
	}
	return subclusters
}

// getSandboxedSubclusters returns the subclusters that belong to the given
// sandbox
func (m *SubclusterFinder) getSandboxedSubclusters(sandbox string) []*vapi.Subcluster {
	subclusters := []*vapi.Subcluster{}
	for i := range m.Vdb.Status.Sandboxes {
		sb := &m.Vdb.Status.Sandboxes[i]
		if sb.Name != sandbox {
			continue
		}
		for _, scName := range sb.Subclusters {
			subclusters = append(subclusters, m.Subclusters[scName])
		}
	}
	return subclusters
}

// filterPodsBySandbox returns a pod list without main cluster pods if a non-empty sandbox named is
// passed in, or pod list with only main cluster pods if no sandbox name is passed
func (m *SubclusterFinder) filterPodsBySandbox(oldPods *corev1.PodList, sandbox string) *corev1.PodList {
	newPods := []corev1.Pod{}
	scMap := m.getSubclusterSandboxStatusMap()
	for i := range oldPods.Items {
		pod := oldPods.Items[i]
		sc := getSubclusterName(pod.Labels)
		sbName, isSandbox := scMap[sc]
		if sandbox == vapi.MainCluster {
			if !isSandbox {
				newPods = append(newPods, pod)
			}
		} else {
			if sbName == sandbox {
				newPods = append(newPods, pod)
			}
		}
	}
	oldPods.Items = newPods
	return oldPods
}

// filterStatefulsetsBySandbox returns a sts list containing sts part of a specific sandbox if a non-empty sandbox named is
// passed in, or a sts list with only main cluster sts if no sandbox name is passed
func (m *SubclusterFinder) filterStatefulsetsBySandbox(oldSts *appsv1.StatefulSetList, sandbox string) *appsv1.StatefulSetList {
	newSts := []appsv1.StatefulSet{}
	scMap := m.getSubclusterSandboxStatusMap()
	for i := range oldSts.Items {
		sts := &oldSts.Items[i]
		sc := getSubclusterName(sts.Labels)
		sbName, isSandbox := scMap[sc]
		if sandbox == vapi.MainCluster {
			if !isSandbox {
				newSts = append(newSts, *sts)
			}
		} else {
			if sbName == sandbox {
				newSts = append(newSts, *sts)
			}
		}
	}
	oldSts.Items = newSts
	return oldSts
}
