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

package aterrors

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ATErrors handles event logging for errors that come back from admintools
type ATErrors struct {
	Writer               events.EVWriter
	VDB                  *vapi.VerticaDB
	GenericFailureReason string // The failure reason when no specific error is found
}

// MakeATErrors will consturct the ATErrors struct
func MakeATErrors(writer events.EVWriter, vdb *vapi.VerticaDB, genericFailureReason string) *ATErrors {
	return &ATErrors{
		Writer:               writer,
		VDB:                  vdb,
		GenericFailureReason: genericFailureReason,
	}
}

const (
	// Amount of time to wait after a restart failed because nodes weren't down yet
	RestartNodesNotDownRequeueWaitTimeInSeconds = 10
)

// LogFailure is called when admintools had attempted an option but
// failed. The command used, along with the output of the command are
// given. This function will parse the output and determine the appropriate
// Event and log message to write.
func (a *ATErrors) LogFailure(cmd, op string, err error) (ctrl.Result, error) {
	switch {
	case isDiskFull(op):
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, events.MgmtFailedDiskFull,
			"'admintools -t %s' failed because of disk full", cmd)
		return ctrl.Result{Requeue: true}, nil

	case areSomeNodesUpForRestart(op):
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, a.GenericFailureReason,
			"Failed while calling 'admintools -t %s'", cmd)
		return ctrl.Result{Requeue: false, RequeueAfter: time.Second * RestartNodesNotDownRequeueWaitTimeInSeconds}, nil

	case cloud.IsEndpointBadError(op):
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, events.CommunalEndpointIssue,
			"Unable to write to the communal endpoint '%s'", a.VDB.Spec.Communal.Endpoint)
		return ctrl.Result{Requeue: true}, nil

	case cloud.IsBucketNotExistError(op):
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, events.S3BucketDoesNotExist,
			"The bucket in the S3 path '%s' does not exist", a.VDB.GetCommunalPath())
		return ctrl.Result{Requeue: true}, nil

	case isCommunalPathNotEmpty(op):
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, events.CommunalPathIsNotEmpty,
			"The communal path '%s' is not empty", a.VDB.GetCommunalPath())
		return ctrl.Result{Requeue: true}, nil

	case isWrongRegion(op):
		a.Writer.Event(a.VDB, corev1.EventTypeWarning, events.S3WrongRegion,
			"You are trying to access your S3 bucket using the wrong region")
		return ctrl.Result{Requeue: true}, nil

	case isConfigParmWrong(op):
		a.Writer.Event(a.VDB, corev1.EventTypeWarning, events.InvalidConfigParm,
			"Invalid communal storage parameter")
		return ctrl.Result{Requeue: true}, nil

	case isS3SseCustomerKeyInvalid(op):
		a.Writer.Event(a.VDB, corev1.EventTypeWarning, events.InvalidS3SseCustomerKey,
			"Invalid key: should be either 32-character plaintext or 44-character base64-encoded")
		return ctrl.Result{Requeue: true}, nil

	case isKerberosAuthError(op):
		a.Writer.Event(a.VDB, corev1.EventTypeWarning, events.KerberosAuthError,
			"Error during keberos authentication")
		return ctrl.Result{Requeue: true}, nil

	case isClusterLeaseNotExpired(op):
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, events.ReviveDBClusterInUse,
			"revive_db failed because the cluster lease has not expired for '%s'",
			a.VDB.GetCommunalPath())
		return ctrl.Result{Requeue: true}, nil

	case isDatabaseNotFound(op):
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, events.ReviveDBNotFound,
			"revive_db failed because the database '%s' could not be found in the communal path '%s'",
			a.VDB.Spec.DBName, a.VDB.GetCommunalPath())
		return ctrl.Result{Requeue: true}, nil

	case isPermissionDeniedError(op):
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, events.ReviveDBPermissionDenied,
			"revive_db failed because of a permission denied error. Verify these paths match the "+
				"ones used by the database: 'DATA,TEMP' => %s, 'DEPOT' => %s, 'CATALOG' => %s",
			a.VDB.Spec.Local.DataPath, a.VDB.Spec.Local.DepotPath, a.VDB.Spec.Local.GetCatalogPath())
		return ctrl.Result{Requeue: true}, nil

	case isNodeCountMismatch(op):
		a.Writer.Event(a.VDB, corev1.EventTypeWarning, events.ReviveDBNodeCountMismatch,
			"revive_db failed because of a node count mismatch")
		return ctrl.Result{Requeue: true}, nil

	default:
		a.Writer.Eventf(a.VDB, corev1.EventTypeWarning, a.GenericFailureReason,
			"Failed while calling 'admintools -t %s'", cmd)
		return ctrl.Result{}, fmt.Errorf("failed mgmt command %s %w", cmd, err)
	}
}

// isDiskFull looks at the admintools output to see if the a diskfull error occurred
func isDiskFull(op string) bool {
	re := regexp.MustCompile(`OSError: \[Errno 28\] No space left on device`)
	return re.FindAllString(op, -1) != nil
}

// areSomeNodesUpForRestart will check for the AT error that restart failed
// because vertica thinks some nodes are still up
func areSomeNodesUpForRestart(op string) bool {
	return strings.Contains(op, "All nodes in the input are not down, can't restart")
}

// isCommunalPathNotEmpty will check the AT output to see if the error is due to
// communal location not being empty during create_db
func isCommunalPathNotEmpty(op string) bool {
	re := regexp.MustCompile(`Communal location \[.+\] is not empty`)
	return re.FindAllString(op, -1) != nil
}

// isWrongRegion will check the error to see if we are accessing the wrong S3 region
func isWrongRegion(op string) bool {
	// We have seen two varieties of errors
	errTexts := []string{
		"You are trying to access your S3 bucket using the wrong region",
		"the region '.+' is wrong; expecting '.+'",
	}

	for i := range errTexts {
		re := regexp.MustCompile(errTexts[i])
		if re.FindAllString(op, -1) != nil {
			return true
		}
	}
	return false
}

// isKerberosAuthError will check if the error is related to Keberos authentication
func isKerberosAuthError(op string) bool {
	re := regexp.MustCompile(`An error occurred during kerberos authentication`)
	return re.FindAllString(op, -1) != nil
}

// isConfigParmWrong will check the error to see if an  invalid parameter was passed to `auth_parms.conf`
func isConfigParmWrong(op string) bool {
	rs := `Invalid configuration parameter .*; aborting configuration change`
	re := regexp.MustCompile(rs)
	return re.FindAllString(op, -1) != nil
}

// isS3SseCustomerKeyInvalid will check the error to see if S3SseCustomerKey is
// invalid(wrong format)
func isS3SseCustomerKeyInvalid(op string) bool {
	return strings.Contains(op, "Invalid S3SseCustomerKey")
}

// isClusterLeaseNotExpired will look at the AT output to see if the revive
// failed because a cluster lease was still active
func isClusterLeaseNotExpired(op string) bool {
	// We use (?s) so that '.' matches newline characters
	rs := `(?s)the communal storage location.*might still be in use.*cluster lease will expire`
	re := regexp.MustCompile(rs)
	return re.FindAllString(op, -1) != nil
}

// isDatabaseNotFound will look at the AT output to see if revive failed because
// it couldn't find the database
func isDatabaseNotFound(op string) bool {
	rs := `(?s)Could not copy file.+: ` +
		`(No such file or directory|.*FileNotFoundException|File not found|.*blob does not exist)`
	re := regexp.MustCompile(rs)
	return re.FindAllString(op, -1) != nil
}

// isPermissionDeniedError will check the AT output to see if the operation
// failed because of permission problems
func isPermissionDeniedError(op string) bool {
	return strings.Contains(op, "Permission Denied")
}

// isNodeCountMismatch will check the AT output to see if the revive failed
// because of a primary node count mismatch
func isNodeCountMismatch(op string) bool {
	if strings.Contains(op, "Error: Node count mismatch") {
		return true
	}
	return strings.Contains(op, "Error: Primary node count mismatch")
}
