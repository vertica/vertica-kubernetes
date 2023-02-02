/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"mgmterrors Suite",
		[]Reporter{printer.NewlineReporter{}})
}

type EventDetails struct {
	EventType string
	Reason    string
	Message   string
}

type TestEVWriter struct {
	RecordedEvents []EventDetails
}

func (t *TestEVWriter) Event(vdb *vapi.VerticaDB, eventtype, reason, message string) {
	d := EventDetails{
		EventType: eventtype,
		Reason:    reason,
		Message:   message,
	}
	if t.RecordedEvents == nil {
		t.RecordedEvents = []EventDetails{}
	}
	t.RecordedEvents = append(t.RecordedEvents, d)
}

func (t *TestEVWriter) Eventf(vdb *vapi.VerticaDB, eventtype, reason, messageFmt string, args ...interface{}) {
	msg := fmt.Sprintf(messageFmt, args...)
	t.Event(vdb, eventtype, reason, msg)
}
