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

package mgmterrors

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

type EventLogger interface {
	// LogFailure is called when the management SDK had attempted a command
	// but failed. The command used, along with the output of the command are
	// given. This function will parse the output and determine the appropriate
	// Event and log message to write. It will also determine the appropriate
	// ctrl.Result to bubble back up.
	LogFailure(cmd, op string, err error) (ctrl.Result, error)
}

// EVWriter is an interface for writing k8s events
type EVWriter interface {
	Event(vdb runtime.Object, eventtype, reason, message string)
	Eventf(vdb runtime.Object, eventtype, reason, messageFmt string, args ...interface{})
}
