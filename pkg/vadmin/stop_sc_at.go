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

package vadmin

import (
	"context"
	"errors"

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopsubcluster"
)

func (a *Admintools) StopSubcluster(_ context.Context, opts ...stopsubcluster.Option) error {
	return errors.New("StopSubcluster is not supported for admintools deployments")
}
