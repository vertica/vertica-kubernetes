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
	"github.com/vertica/vcluster/vclusterops"
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
	if ok := errors.As(err, &vproblem); ok {
		return v.logRfc7807Failure(cmd, vproblem)
	}
	clusterLeaseNotExpiredError := &vclusterops.ClusterLeaseNotExpiredError{}
	if ok := errors.As(err, &clusterLeaseNotExpiredError); ok {
		return v.logClusterLeaseNotExpiredError(clusterLeaseNotExpiredError)
	}
	return v.logGenericFailure(cmd, err)
}

func (v *vcErrors) logGenericFailure(cmd string, err error) (ctrl.Result, error) {
	// Unable to know exactly what the error is.
	// We log the generic failure reason of the
	// given command.
	v.Log.Error(err, "vclusterOps command failed", "cmd", cmd)
	v.EVWriter.Eventf(v.VDB, corev1.EventTypeWarning,
		v.GenericFailureReason, fmt.Sprintf("Failed when calling %s", cmd))
	return ctrl.Result{}, err
}

func (v *vcErrors) logRfc7807Failure(cmd string, vproblem *rfc7807.VProblem) (ctrl.Result, error) {
	v.Log.Error(vproblem, "vclusterOps command failed", "cmd", cmd,
		"type", vproblem.Type, "title", vproblem.Title, "detail", vproblem.Detail,
		"host", vproblem.Host, "status", vproblem.Status)
	reason, ok := rfc7807TypeToEventMap[vproblem.Type]
	if !ok {
		reason = v.GenericFailureReason
	}
	v.EVWriter.Eventf(v.VDB, corev1.EventTypeWarning, reason, vproblem.Title)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed command %s: %w", cmd, vproblem)
	}
	// All known errors we return with requeue set to true.
	return ctrl.Result{Requeue: true}, nil
}

func (v *vcErrors) logClusterLeaseNotExpiredError(err *vclusterops.ClusterLeaseNotExpiredError) (ctrl.Result, error) {
	v.Log.Info("vclusterOps command failed because the cluster lease was not expired", "msg", err.Error())
	v.EVWriter.Eventf(v.VDB, corev1.EventTypeWarning, events.ReviveDBClusterInUse,
		"revive_db failed because the cluster lease has not expired for '%s'",
		v.VDB.GetCommunalPath())
	return ctrl.Result{Requeue: true}, nil
}
