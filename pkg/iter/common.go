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

package iter

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// listObjectsOwnedByOperator will return all objects of a specific type that
// are owned by the operator.  This includes objects like statefulsets or
// service objects.  The type is derived from what kind of list is passed in.
// We find objects the operator owns by using a set of labels that the operator
// sets with each object it creates.
func listObjectsOwnedByOperator(ctx context.Context, cl client.Client, vdb *vapi.VerticaDB, list client.ObjectList) error {
	labelSel := labels.SelectorFromSet(builder.MakeOperatorLabels(vdb))
	listOpts := &client.ListOptions{
		Namespace:     vdb.Namespace,
		LabelSelector: labelSel,
	}
	return cl.List(ctx, list, listOpts)
}
