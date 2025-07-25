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
	// AnnotationsToRemove are the annotations that you want to remove
	AnnotationsToRemove []string
	// LabelsToRemove are the labels that you want to remove
	LabelsToRemove []string
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

		var annotationsChanged, labelsChanged bool
		objAnnotations, changed1 := deleteFromMap(obj.GetAnnotations(), chgs.AnnotationsToRemove)
		if changed1 {
			annotationsChanged = true
			obj.SetAnnotations(objAnnotations)
		}
		objAnnotations, changed1 = addOrReplaceMap(objAnnotations, chgs.NewAnnotations)
		if changed1 {
			annotationsChanged = true
			obj.SetAnnotations(objAnnotations)
		}

		objLabels, changed2 := deleteFromMap(obj.GetLabels(), chgs.LabelsToRemove)
		if changed2 {
			labelsChanged = true
			obj.SetLabels(objLabels)
		}

		objLabels, changed2 = addOrReplaceMap(objLabels, chgs.NewLabels)
		if changed2 {
			labelsChanged = true
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

func MetaUpdateWithAnnotations(ctx context.Context, cl client.Client, nm types.NamespacedName,
	obj client.Object, chgs map[string]string) (bool, error) {
	return metaUpdateWithMap(ctx, cl, nm, obj, chgs, false)
}

func metaUpdateWithMap(ctx context.Context, cl client.Client, nm types.NamespacedName,
	obj client.Object, chgs map[string]string, isLabel bool) (bool, error) {
	metaChgs := MetaChanges{}
	if isLabel {
		metaChgs.NewLabels = chgs
	} else {
		metaChgs.NewAnnotations = chgs
	}
	return MetaUpdate(ctx, cl, nm, obj, metaChgs)
}

func addOrReplaceMap(oldMap, newMap map[string]string) (map[string]string, bool) {
	mapChanged := false
	for k, v := range newMap {
		if oldMap[k] != v {
			if oldMap == nil {
				oldMap = map[string]string{}
			}
			oldMap[k] = v
			mapChanged = true
		}
	}
	return oldMap, mapChanged
}

func deleteFromMap(oldMap map[string]string, keysToDelete []string) (map[string]string, bool) {
	mapChanged := false
	for _, k := range keysToDelete {
		if _, exists := oldMap[k]; exists {
			delete(oldMap, k)
			mapChanged = true
		}
	}
	return oldMap, mapChanged
}

func UpdateAnnotation(annotationField, annotationValue string, obj client.Object, ctx context.Context,
	k8sClient client.Client, nm types.NamespacedName) (bool, error) {
	anns := map[string]string{
		annotationField: annotationValue,
	}
	return MetaUpdateWithAnnotations(ctx, k8sClient, nm, obj, anns)
}
