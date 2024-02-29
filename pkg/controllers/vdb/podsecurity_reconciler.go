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
	"regexp"
	"strconv"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PodSecurityReconciler will handle picking the UID/GID/fsGroup for the pods.
type PodSecurityReconciler struct {
	VRec          *VerticaDBReconciler
	Vdb           *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log           logr.Logger
	InitFSGroupID int64
	InitRunAsUser int64
}

const (
	DefaultRunAsUser              = 5000
	DefaultFSGroupID              = 5000
	OpenShiftGroupRangeAnnotation = "openshift.io/sa.scc.supplemental-groups"
	OpenShiftUIDRangeAnnotation   = "openshift.io/sa.scc.uid-range"
)

func MakePodSecurityReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &PodSecurityReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("UIDSelectorReconciler"),
	}
}

// Reconcile will ensure that a serviceAccount, role and rolebindings exists for
// the vertica pods.
func (p *PodSecurityReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// We only do this before we initialize the database.
	isSet := p.Vdb.IsStatusConditionTrue(vapi.DBInitialized)
	if isSet {
		return ctrl.Result{}, nil
	}

	psc := p.Vdb.Spec.PodSecurityContext
	if psc != nil && psc.FSGroup != nil && psc.RunAsUser != nil {
		p.Log.Info("PodSecurityContext already setup", "fsGroup", psc.FSGroup, "uid", psc.RunAsUser)
		return ctrl.Result{}, nil
	}
	if psc == nil {
		psc = &corev1.PodSecurityContext{}
	}

	// Figure out the init values if none are specified. This is deployment
	// specific. In environment like OpenShift we need to pull a value from a
	// defined range.
	err := p.loadInitValues(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	fsGroup := psc.FSGroup
	if fsGroup == nil {
		fsGroup = &p.InitFSGroupID
	}

	runAsUser := psc.RunAsUser
	if runAsUser == nil {
		runAsUser = &p.InitRunAsUser
	}

	return ctrl.Result{}, p.updatePodSecurityContext(ctx, fsGroup, runAsUser)
}

func (p *PodSecurityReconciler) updatePodSecurityContext(ctx context.Context, fsGroup, runAsUser *int64) error {
	nm := p.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := p.VRec.Client.Get(ctx, nm, p.Vdb); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		psc := p.Vdb.Spec.PodSecurityContext
		if psc == nil {
			psc = &corev1.PodSecurityContext{}
			p.Vdb.Spec.PodSecurityContext = psc
		}
		psc.FSGroup = fsGroup
		psc.RunAsUser = runAsUser
		p.Log.Info("Updating PodSecurityContext", "fsGroup", fsGroup, "runAsUser", runAsUser)
		return p.VRec.Client.Update(ctx, p.Vdb)
	})
}

// loadInitValues reads namespace metadata to determine if we are running in
// OpenShift. This will set u.IsOpenShift and u.NamespaceAnnotations on exit.
func (p *PodSecurityReconciler) loadInitValues(ctx context.Context) error {
	p.InitFSGroupID = DefaultFSGroupID
	p.InitRunAsUser = DefaultRunAsUser

	if !vmeta.UseVClusterOps(p.Vdb.Annotations) {
		return nil
	}

	nmAnnotations, err := p.getNamespaceAnnotations(ctx)
	if err != nil {
		return err
	}
	p.InitFSGroupID = getIDFromAnnotationIfAvailable(
		nmAnnotations, OpenShiftGroupRangeAnnotation, DefaultFSGroupID)
	p.InitRunAsUser = getIDFromAnnotationIfAvailable(
		nmAnnotations, OpenShiftUIDRangeAnnotation, DefaultRunAsUser)
	return nil
}

func getIDFromAnnotationIfAvailable(ann map[string]string, keyName string, defaultVal int64) int64 {
	rangeVal, isOpenShift := ann[keyName]
	if isOpenShift {
		id, err := parseOpenShiftUIDRange(rangeVal)
		// Eat the error, we will just continue to use the default
		if err != nil {
			return defaultVal
		}
		return id
	}
	return defaultVal
}

func (p *PodSecurityReconciler) getNamespaceAnnotations(ctx context.Context) (map[string]string, error) {
	// If the operator is scoped at the namespace level, we won't have
	// privileges to read the namespace since that is a cluster scoped object.
	// So, we cannot get any annotations.
	if opcfg.AreControllersNamespaceScoped() {
		p.Log.Info("unable to read namespace annotations due to namespace scope")
		return nil, nil
	}

	nm := types.NamespacedName{
		Name: p.Vdb.Namespace,
	}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: p.Vdb.Namespace,
		},
	}
	err := p.VRec.Client.Get(ctx, nm, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch namespace %s: %w", p.Vdb.Namespace, err)
	}
	return namespace.Annotations, nil
}

func parseOpenShiftUIDRange(rng string) (int64, error) {
	// The range format is: <first_id>/<id_pool_size> or <first_id>-<last_id>.
	// For example, 1001070000/10000.
	re := regexp.MustCompile(`(^\d+)[/-]`)
	m := re.FindStringSubmatch(rng)
	const ExpMatch = 2 // [whole match, group 1]
	if len(m) == ExpMatch {
		const Base10 = 10
		return strconv.ParseInt(m[1], Base10, 0)
	}
	return 0, fmt.Errorf("unexpected format for UID range: %s", rng)
}
