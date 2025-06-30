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

package vdbstatus

import (
	"context"
	"reflect"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Update will set status fields in the VerticaDB.  It handles retry for
// transient errors like when update fails because another client updated the
// VerticaDB.
func Update(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, updateFunc func(*vapi.VerticaDB) error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest to minimize the chance of getting a conflict error.
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
		if err := clnt.Get(ctx, nm, vdb); err != nil {
			return err
		}

		// We will calculate the status for the vdb object. This update is done in
		// place. If anything differs from the copy then we will do a single update.
		vdbChg := vdb.DeepCopy()

		// Refresh the status using the users provided function
		if err := updateFunc(vdbChg); err != nil {
			return err
		}

		if !reflect.DeepEqual(vdb.Status, vdbChg.Status) {
			vdbChg.Status.DeepCopyInto(&vdb.Status)
			if err := clnt.Status().Update(ctx, vdb); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateCondition will update a condition status
// This is a no-op if the status condition is already set.  The input vdb will
// be updated with the status condition.
func UpdateCondition(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, condition *metav1.Condition) error {
	// refreshConditionInPlace will update the status condition in vdb.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vdb *vapi.VerticaDB) error {
		meta.SetStatusCondition(&vdb.Status.Conditions, *condition)
		return nil
	}

	return Update(ctx, clnt, vdb, refreshConditionInPlace)
}

// UpdateConditions will update multiple condition statuses
func UpdateConditions(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, conditions []*metav1.Condition) error {
	// refreshConditionInPlace will update the status conditions in vdb.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vdb *vapi.VerticaDB) error {
		for _, condition := range conditions {
			meta.SetStatusCondition(&vdb.Status.Conditions, *condition)
		}
		return nil
	}

	return Update(ctx, clnt, vdb, refreshConditionInPlace)
}

// UpdateSecretRef will update a secret status. There is no-op if the status secret is already set.
func UpdateSecretRef(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, sRef *vapi.SecretRef) error {
	// refreshConditionInPlace will update the status secretRef in vdb.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vdb *vapi.VerticaDB) error {
		vapi.SetSecretRef(&vdb.Status.SecretRefs, *sRef)
		return nil
	}

	return Update(ctx, clnt, vdb, refreshConditionInPlace)
}

// UpdateTLSModes will update a TLS modes in status. There is no-op if the status TLS mode is already set.
func UpdateTLSModes(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, tlsModes []*vapi.TLSMode) error {
	// refreshConditionInPlace will update the status secretRef in vdb.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vdb *vapi.VerticaDB) error {
		for _, tlsMode := range tlsModes {
			vapi.SetTLSMode(&vdb.Status.TLSModes, *tlsMode)
		}
		return nil
	}
	return Update(ctx, clnt, vdb, refreshConditionInPlace)
}

// UpdateSecretRefs will update multiple secret status. There is no-op if the status secret is already set.
func UpdateSecretRefs(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, sRefs []*vapi.SecretRef) error {
	// refreshConditionInPlace will update the status secretRef in vdb.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vdb *vapi.VerticaDB) error {
		for _, sRef := range sRefs {
			vapi.SetSecretRef(&vdb.Status.SecretRefs, *sRef)
		}
		return nil
	}
	return Update(ctx, clnt, vdb, refreshConditionInPlace)
}

// SetUpgradeStatusMessage will set the upgrade status message.  The
// input vdb will be updated with the status message.
func SetUpgradeStatusMessage(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, msg string) error {
	return Update(ctx, clnt, vdb, func(vdb *vapi.VerticaDB) error {
		vdb.Status.UpgradeStatus = msg
		return nil
	})
}

// SetSandboxUpgradeState will set the sandbox upgrade state and update the input vdb
func SetSandboxUpgradeState(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, sbName string,
	state *vapi.SandboxUpgradeState) error {
	return Update(ctx, clnt, vdb, func(vdb *vapi.VerticaDB) error {
		sb, err := vdb.GetSandboxStatusCheck(sbName)
		if err != nil {
			return err
		}
		sb.UpgradeState = *state
		return nil
	})
}
