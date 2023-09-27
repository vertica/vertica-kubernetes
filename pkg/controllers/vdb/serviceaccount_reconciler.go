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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ServiceAccountReconciler will handle generation of the service account
type ServiceAccountReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

func MakeServiceAccountReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &ServiceAccountReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("ServiceAccountReconciler"),
	}
}

// Reconcile will ensure that a serviceAccount, role and rolebindings exists for
// the vertica pods.
func (s *ServiceAccountReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// If a serviceAccount name was specified and it exists, then we can exit
	// this reconciler early.
	if s.Vdb.Spec.ServiceAccountName != "" {
		if exists, err := s.hasUserProvidedServiceAccount(ctx, s.Vdb.Spec.ServiceAccountName); exists || err != nil {
			return ctrl.Result{}, err
		}
	}

	rbacFinder := iter.MakeRBACFinder(s.VRec.Client, s.Vdb)
	exists, sa, err := rbacFinder.FindServiceAccount(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to lookup serviceaccount: %w", err)
	}
	if !exists {
		sa, err = s.createServiceAccount(ctx)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create serviceaccont: %w", err)
		}
	}

	var role *rbacv1.Role
	exists, role, err = rbacFinder.FindRole(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to lookup role: %w", err)
	}
	if !exists {
		role, err = s.createRole(ctx)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create role: %w", err)
		}
	}

	exists, _, err = rbacFinder.FindRoleBinding(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to lookup rolebinding: %w", err)
	}
	if !exists {
		err = s.createRoleBinding(ctx, sa, role)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create rolebinding: %w", err)
		}
	}

	return ctrl.Result{}, s.saveServiceAccountNameInVDB(ctx, sa.Name)
}

// createServiceAccount will create a new service account to be used for the vertica pods
func (s *ServiceAccountReconciler) createServiceAccount(ctx context.Context) (*corev1.ServiceAccount, error) {
	isController := true
	blockOwnerDeletion := false
	sa := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   s.Vdb.Namespace,
			Annotations: builder.MakeAnnotationsForObject(s.Vdb),
			Labels:      builder.MakeCommonLabels(s.Vdb, nil, false),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         vapi.GroupVersion.String(),
					Kind:               vapi.VerticaDBKind,
					Name:               s.Vdb.Name,
					UID:                s.Vdb.GetUID(),
					Controller:         &isController,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
		},
	}
	// Only generate a name if one wasn't already specified in the vdb
	if s.Vdb.Spec.ServiceAccountName == "" {
		sa.GenerateName = fmt.Sprintf("%s-sa-", s.Vdb.Name)
	} else {
		sa.Name = s.Vdb.Spec.ServiceAccountName
	}
	err := s.VRec.Client.Create(ctx, &sa)
	if err != nil {
		err = fmt.Errorf("failed to create serviceaccount with generated name %s for VerticaDB: %w",
			sa.ObjectMeta.GenerateName, err)
	}
	s.Log.Info("serviceaccount created", "name", sa.ObjectMeta.Name)
	return &sa, err
}

// createRole will create a Role suitable for running vertica pods. The created role is returned.
func (s *ServiceAccountReconciler) createRole(ctx context.Context) (*rbacv1.Role, error) {
	isController := true
	blockOwnerDeletion := false
	role := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-role-", s.Vdb.Name),
			Namespace:    s.Vdb.Namespace,
			Annotations:  builder.MakeAnnotationsForObject(s.Vdb),
			Labels:       builder.MakeCommonLabels(s.Vdb, nil, false),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         vapi.GroupVersion.String(),
					Kind:               vapi.VerticaDBKind,
					Name:               s.Vdb.Name,
					UID:                s.Vdb.GetUID(),
					Controller:         &isController,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
		},
		Rules: []rbacv1.PolicyRule{
			// Any policy changes here must be kept insync with:
			// config/samples/vertica-server-role.yaml
			{
				// We need to allow vertica pods to read secrets directly from
				// the API. This will be used by the NMA and vcluster CLI to
				// read the cert to communicate with.
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs: []string{
					"get",
					"list",
				},
			},
		},
	}
	err := s.VRec.Client.Create(ctx, &role)
	if err != nil {
		err = fmt.Errorf("failed to create role with generated name %s for VerticaDB: %w",
			role.ObjectMeta.GenerateName, err)
	}
	s.Log.Info("role created", "name", role.ObjectMeta.Name)
	return &role, err
}

// createRoleBinding will create a new RoleBinding that is owned by the
// operator. It will bind the given Role and ServiceAccount together.
func (s *ServiceAccountReconciler) createRoleBinding(ctx context.Context, sa *corev1.ServiceAccount, role *rbacv1.Role) error {
	isController := true
	blockOwnerDeletion := false
	rolebinding := rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-rolebinding-", s.Vdb.Name),
			Namespace:    s.Vdb.Namespace,
			Annotations:  builder.MakeAnnotationsForObject(s.Vdb),
			Labels:       builder.MakeCommonLabels(s.Vdb, nil, false),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         vapi.GroupVersion.String(),
					Kind:               vapi.VerticaDBKind,
					Name:               s.Vdb.Name,
					UID:                s.Vdb.GetUID(),
					Controller:         &isController,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     role.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      sa.Name,
				Namespace: sa.Namespace,
			},
		},
	}
	err := s.VRec.Client.Create(ctx, &rolebinding)
	if err != nil {
		return fmt.Errorf("failed to create rolebinding with generated name %s for VerticaDB: %w",
			role.ObjectMeta.GenerateName, err)
	}
	s.Log.Info("rolebinding created", "name", rolebinding.ObjectMeta.Name)
	return nil
}

// hasUserProvidedServiceAccount will check if the given serviceAccount name
// exists and was user provided.
func (s *ServiceAccountReconciler) hasUserProvidedServiceAccount(ctx context.Context, saName string) (bool, error) {
	sa := corev1.ServiceAccount{}
	nm := names.GenNamespacedName(s.Vdb, saName)
	if err := s.VRec.Client.Get(ctx, nm, &sa); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	// Check if the serviceAccount has the expected labels. If it doesn't, then
	// we assume the service account is user provided.
	if sa.Labels == nil {
		return true, nil
	}
	expLabels := builder.MakeCommonLabels(s.Vdb, nil, false)
	for k, v := range expLabels {
		if sa.Labels[k] != v {
			return true, nil
		}
	}
	return false, nil
}

// saveServiceAccountNameInVDB will store the given serviceAccountName in the VerticaDB.
func (s *ServiceAccountReconciler) saveServiceAccountNameInVDB(ctx context.Context, saName string) error {
	nm := s.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// We refresh the EventTrigger incase we need to do a retry. But we
		// assume it's sufficiently populated to have a name.
		if err := s.VRec.Client.Get(ctx, nm, s.Vdb); err != nil {
			return err
		}

		s.Vdb.Spec.ServiceAccountName = saName
		s.Log.Info("Updating serviceAccountName in VerticaDB", "name", saName)
		return s.VRec.Client.Update(ctx, s.Vdb)
	})
}
