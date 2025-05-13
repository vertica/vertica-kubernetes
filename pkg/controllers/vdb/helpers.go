/*
 (c) Copyright [2021-2025] Open Text.
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
	"fmt"

	"github.com/go-logr/logr"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
)

func traceActorReconcile(actor controllers.ReconcileActor, log logr.Logger, reason string) {
	msg := fmt.Sprintf("starting actor for %s", reason)
	log.Info(msg, "name", fmt.Sprintf("%T", actor))
}
