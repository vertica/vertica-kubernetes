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

package vk8s

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MetaChanges contains the metadata changes you wish to apply through
// MetaUpdate
type MetaChanges struct {
	// New labels that you want to set or overwrite
	NewLabels map[string]string
	// New annotations that you want to set or overwrite
	NewAnnotations map[string]string
}

// MetaUpdate is a general purpose function to add changes to the metadata of a
// given object. The object could be any k8s object (i.e Pod, VerticaDB, etc.).
// The first bool parameter is used to indicate if an update did occur.
func MetaUpdate(ctx context.Context, cl client.Client, nm types.NamespacedName, obj client.Object, chgs MetaChanges) (bool, error) {
	updated := false
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := cl.Get(ctx, nm, obj); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		annotationsChanged := false
		objAnnotations := obj.GetAnnotations()
		for k, v := range chgs.NewAnnotations {
			if objAnnotations[k] != v {
				if objAnnotations == nil {
					objAnnotations = map[string]string{}
				}
				objAnnotations[k] = v
				annotationsChanged = true
			}
		}
		if annotationsChanged {
			obj.SetAnnotations(objAnnotations)
		}

		labelsChanged := false
		objLabels := obj.GetLabels()
		for k, v := range chgs.NewLabels {
			if objLabels[k] != v {
				if objLabels == nil {
					objLabels = map[string]string{}
				}
				objLabels[k] = v
				labelsChanged = true
			}
		}
		if labelsChanged {
			obj.SetLabels(objLabels)
		}

		if annotationsChanged || labelsChanged {
			err := cl.Update(ctx, obj)
			if err == nil {
				updated = true
			}
			return err
		}
		return nil
	})
	return updated, err
}
