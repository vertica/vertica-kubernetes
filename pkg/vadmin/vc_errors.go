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

package vadmin

import (
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/vertica/vcluster/rfc7807"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type vcErrors struct {
	VDB                  *vapi.VerticaDB
	Log                  logr.Logger
	GenericFailureReason string
	EVWriter             events.EVWriter
}

// rfc7807TypeToEventMap is a mapping from known rfc7807 errors to the event
// reason type. Some errors are intentionally omitted. These will use the
// generic failure reason that is setup for the command.
var rfc7807TypeToEventMap = map[string]string{
	// rfc7807.GenericBootstrapCatalogFailure.Type left out
	rfc7807.CommunalStorageNotEmpty.Type: events.CommunalPathIsNotEmpty,
	// rfc7807.CommunalStoragePathInvalid.Type left out
	rfc7807.CommunalRWAccessError.Type: events.CommunalEndpointIssue,
	rfc7807.CommunalAccessError.Type:   events.CommunalEndpointIssue,
}

func (v *vcErrors) LogFailure(cmd string, err error) (ctrl.Result, error) {
	vproblem := &rfc7807.VProblem{}
	if ok := errors.As(err, &vproblem); !ok {
		// Unable to know exactly what the error is. Just return as-is and don't
		// do any k8s event logging
		v.Log.Error(err, "vclusterOps command failed", "cmd", cmd)
		return ctrl.Result{}, err
	}
	v.Log.Error(err, "vclusterOps command failed", "cmd", cmd,
		"type", vproblem.Type, "title", vproblem.Title, "detail", vproblem.Detail,
		"host", vproblem.Host, "status", vproblem.Status)
	reason, ok := rfc7807TypeToEventMap[vproblem.Type]
	if !ok {
		reason = v.GenericFailureReason
	}
	v.EVWriter.Eventf(v.VDB, corev1.EventTypeWarning, reason, vproblem.Title)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed command %s: %w", cmd, err)
	}
	// All known errors we return with requeue set to true.
	return ctrl.Result{Requeue: true}, nil
}
