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
	"k8s.io/apimachinery/pkg/types"
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
	// Find will return a list of objects without filtering based on the
	// sandbox
	FindSkipSandboxFilter
	// Find all subclusters, both in the vdb and not in the vdb.
	FindAll = FindInVdb | FindNotInVdb
	// Find all subclusters, both in the vdb and not in the vdb, regardless
	// of the sandbox they belong to.
	FindAllAcrossSandboxes = FindAll | FindSkipSandboxFilter
	// Find subclusters not in vdb regardless of the sandbox they belong to.
	FindNotInVdbAcrossSandboxes = FindNotInVdb | FindSkipSandboxFilter
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
func (m *SubclusterFinder) FindStatefulSets(ctx context.Context, flags FindFlags, sandbox string) (*appsv1.StatefulSetList, error) {
	sts := &appsv1.StatefulSetList{}
	if err := m.buildObjList(ctx, sts, flags, sandbox); err != nil {
		return nil, err
	}
	if flags&FindSorted != 0 {
		sort.Slice(sts.Items, func(i, j int) bool {
			return sts.Items[i].Name < sts.Items[j].Name
		})
	}
	return sts, nil
}

// FindServices returns service objects that are in use for subclusters
func (m *SubclusterFinder) FindServices(ctx context.Context, flags FindFlags, sandbox string) (*corev1.ServiceList, error) {
	svcs := &corev1.ServiceList{}
	if err := m.buildObjList(ctx, svcs, flags, sandbox); err != nil {
		return nil, err
	}
	if flags&FindSorted != 0 {
		sort.Slice(svcs.Items, func(i, j int) bool {
			return svcs.Items[i].Name < svcs.Items[j].Name
		})
	}
	return svcs, nil
}

// FindDeployments returns deployment objects that are in use for subclusters
func (m *SubclusterFinder) FindDeployments(ctx context.Context, flags FindFlags, sandbox string) (*appsv1.DeploymentList, error) {
	deploy := &appsv1.DeploymentList{}
	if err := m.buildObjList(ctx, deploy, flags, sandbox); err != nil {
		return nil, err
	}
	if flags&FindSorted != 0 {
		sort.Slice(deploy.Items, func(i, j int) bool {
			return deploy.Items[i].Name < deploy.Items[j].Name
		})
	}
	return deploy, nil
}

// FindConfigMaps returns config map objects that are in use for subclusters
func (m *SubclusterFinder) FindConfigMaps(ctx context.Context, flags FindFlags, sandbox string) (*corev1.ConfigMapList, error) {
	cm := &corev1.ConfigMapList{}
	if err := m.buildObjList(ctx, cm, flags, sandbox); err != nil {
		return nil, err
	}
	if flags&FindSorted != 0 {
		sort.Slice(cm.Items, func(i, j int) bool {
			return cm.Items[i].Name < cm.Items[j].Name
		})
	}
	return cm, nil
}

// FindPods returns pod objects that are are used to run Vertica.  It limits the
// pods that were created by the VerticaDB object.
func (m *SubclusterFinder) FindPods(ctx context.Context, flags FindFlags, sandbox string) (*corev1.PodList, error) {
	pods := &corev1.PodList{}
	if err := m.buildObjList(ctx, pods, flags, sandbox); err != nil {
		return nil, err
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

	if flags&FindInVdb != 0 {
		// This is true when we want to get all subclusters without any
		// sandbox distinction
		ignoreSandbox := flags&FindSkipSandboxFilter != 0
		subclusters = append(subclusters, m.getVdbSubclusters(sandbox, ignoreSandbox)...)
	}

	if flags&FindNotInVdb != 0 || flags&FindExisting != 0 {
		missingSts, err := m.FindStatefulSets(ctx, flags & ^FindInVdb, sandbox)
		if err != nil {
			return nil, err
		}

		// We will convert each statefulset into a vapi.Subcluster stub object.  We
		// only fill in the name.  Size is intentionally left zero as this is an
		// indication the subcluster is being removed.
		for i := range missingSts.Items {
			scName := missingSts.Items[i].Labels[vmeta.SubclusterNameLabel]
			subclusters = append(subclusters, &vapi.Subcluster{
				Name: scName,
				Size: 0,
				Annotations: map[string]string{
					vmeta.StsNameOverrideAnnotation: missingSts.Items[i].ObjectMeta.Name,
					// This will let the operator know if the subcluster is zombie
					vmeta.SandboxNameLabel: missingSts.Items[i].Labels[vmeta.SandboxNameLabel],
				},
			})
		}
	}

	if flags&FindSorted != 0 {
		sort.Slice(subclusters, func(i, j int) bool {
			return subclusters[i].Name < subclusters[j].Name
		})
	}
	return subclusters, nil
}

// hasSubclusterLabelFromVdb returns true if the given set of labels include a
// subcluster that is in the vdb. Note, for pods, objLabels will be from the
// statefulset. So, it is safe to use SubclusterNameLabel.
func (m *SubclusterFinder) hasSubclusterLabelFromVdb(objLabels map[string]string) bool {
	scName := objLabels[vmeta.SubclusterNameLabel]
	_, ok := m.Subclusters[scName]
	return ok
}

// buildObjList will populate list with an object type owned by the operator.
// Caller can use flags to return a list of all objects, only those in the vdb,
// or only those not in the vdb.
func (m *SubclusterFinder) buildObjList(ctx context.Context, list client.ObjectList, flags FindFlags, sandbox string) error {
	if err := listObjectsOwnedByOperator(ctx, m.Client, m.Vdb, list); err != nil {
		return err
	}
	ignoreSandbox := flags&FindSkipSandboxFilter != 0
	rawObjs := []runtime.Object{}
	if err := meta.EachListItem(list, func(obj runtime.Object) error {
		l, err := m.getLabelsFromObject(ctx, obj)
		if err != nil {
			return err
		}
		if flags&FindAll == FindAll {
			// When FindAll is passed, we want the entire list to be returned,
			// but still want to filter out objects that do not belong to the given
			// sandbox or main cluster.
			if ignoreSandbox || !shouldSkipBasedOnSandboxState(l, sandbox) {
				rawObjs = append(rawObjs, obj)
			}
			return nil
		}

		// Skip if object is not subcluster specific.  This is necessary for objects like
		// the headless service object that is cluster wide.
		if !hasSubclusterNameLabel(l) {
			return nil
		}

		// Skip if the object does not belong to the given sandbox
		if !ignoreSandbox && shouldSkipBasedOnSandboxState(l, sandbox) {
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

// shouldSkipBasedOnSandboxState returns true if the object whose labels
// is passed does not belong to the given sandbox or main cluster. Note, for a
// pod, the labels passed in will be from the statefulset. So, it is fine to use
// SubclusterNameLabel.
func shouldSkipBasedOnSandboxState(l map[string]string, sandbox string) bool {
	return l[vmeta.SandboxNameLabel] != sandbox
}

// hasSubclusterNameLabel returns true if there exists a label that indicates
// the object is for a subcluster. Note, for a pod, the labels passed in will be
// from the statefulset. So, it is fine to use SubclusterNameLabel.
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

// getLabelsFromObject will extract the labels from a k8s object.
// If labels were not found then false is return for bool output.
//
//nolint:gocritic
func (m *SubclusterFinder) getLabelsFromObject(ctx context.Context, obj runtime.Object) (map[string]string, error) {
	if sts, ok := obj.(*appsv1.StatefulSet); ok {
		return sts.Labels, nil
	} else if svc, ok := obj.(*corev1.Service); ok {
		return svc.Labels, nil
	} else if deploy, ok := obj.(*appsv1.Deployment); ok {
		return deploy.Labels, nil
	} else if cm, ok := obj.(*corev1.ConfigMap); ok {
		return cm.Labels, nil
	} else if pod, ok := obj.(*corev1.Pod); ok {
		// Instead of retrieving labels directly from the pod, we retrieve them
		// from the StatefulSet that owns the pod. The labels we need, such as
		// the subcluster name or sandbox, aren't present in the pods
		// themselves. These values may change, and maintaining them becomes
		// challenging because any modifications to the StatefulSet's pod
		// template trigger a rolling update. Therefore, the solution is to
		// obtain the labels from the owning object.
		stsName, err := getStatefulSetOwnerName(pod)
		if err != nil {
			return nil, err
		}
		sts := appsv1.StatefulSet{}
		err = m.Client.Get(ctx, stsName, &sts)
		if err != nil {
			return nil, err
		}
		return sts.Labels, nil
	}
	return nil, fmt.Errorf("unexpected object type: %v", obj.GetObjectKind().GroupVersionKind())
}

func getStatefulSetOwnerName(pod *corev1.Pod) (types.NamespacedName, error) {
	if stsName, found := pod.Labels[vmeta.SubclusterSelectorLabel]; found {
		return types.NamespacedName{
			Namespace: pod.Namespace,
			Name:      stsName,
		}, nil
	}
	return types.NamespacedName{},
		fmt.Errorf("could not find statefulset owner for pod %q in namespace %q", pod.Name, pod.Namespace)
}

// getVdbSubclusters returns the subclusters that are in the vdb
func (m *SubclusterFinder) getVdbSubclusters(sandbox string, ignoreSandbox bool) []*vapi.Subcluster {
	subclusters := []*vapi.Subcluster{}
	scMap := m.Vdb.GenSubclusterSandboxStatusMap()
	for i := range m.Vdb.Spec.Subclusters {
		sc := &m.Vdb.Spec.Subclusters[i]
		sbName := scMap[sc.Name]
		if ignoreSandbox || sbName == sandbox {
			subclusters = append(subclusters, sc)
		}
	}
	return subclusters
}
