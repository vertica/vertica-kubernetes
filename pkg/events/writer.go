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

package events

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

// EVWriter is an interface for writing k8s events
type EVWriter interface {
	Event(vdb runtime.Object, eventtype, reason, message string)
	Eventf(vdb runtime.Object, eventtype, reason, messageFmt string, args ...interface{})
}

// Writer is a concrete class that implements EVWriter
type Writer struct {
	Log   logr.Logger
	EVRec record.EventRecorder
}

// Event a wrapper for Event() that also writes a log entry
func (w Writer) Event(vdb runtime.Object, eventtype, reason, message string) {
	w.Log.Info("Event logging", "eventtype", eventtype, "reason", reason, "message", message)
	w.EVRec.Event(vdb, eventtype, reason, message)
}

// Eventf is a wrapper for Eventf() that also writes a log entry
func (w Writer) Eventf(vdb runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	w.Log.Info("Event logging", "eventtype", eventtype, "reason", reason, "message", fmt.Sprintf(messageFmt, args...))
	w.EVRec.Eventf(vdb, eventtype, reason, messageFmt, args...)
}
