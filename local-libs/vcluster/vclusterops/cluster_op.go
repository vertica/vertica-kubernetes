/*
 (c) Copyright [2023-2024] Open Text.
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

// vclusterops is a Go library to administer a Vertica cluster with HTTP RESTful
// interfaces. These interfaces are exposed through the Node Management Agent
// (NMA) and an HTTPS service embedded in the server. With this library you can
// perform administrator-level operations, including: creating a database,
// scaling up/down, restarting the cluster, and stopping the cluster.
package vclusterops

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/theckman/yacspin"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* Op and host http result status
 */

// resultStatus is the data type for the status of
// ClusterOpResult and hostHTTPResult
type resultStatus int

var wrongCredentialErrMsg = []string{"Wrong password", "Wrong certificate"}

const (
	SUCCESS      resultStatus = 0
	FAILURE      resultStatus = 1
	EXCEPTION    resultStatus = 2
	EOFEXCEPTION resultStatus = 3
)

const (
	GetMethod    = "GET"
	PutMethod    = "PUT"
	PostMethod   = "POST"
	DeleteMethod = "DELETE"
)

const (
	// track endpoint versions and the current version
	NMAVersion1    = "v1/"
	HTTPVersion1   = "v1/"
	NMACurVersion  = NMAVersion1
	HTTPCurVersion = HTTPVersion1
)

const (
	SuccessResult      = "SUCCESS"
	FailureResult      = "FAILURE"
	ExceptionResult    = "EXCEPTION"
	EOFExceptionResult = "EOF_EXCEPTION"
)

const (
	SuccessCode            = 200
	MultipleChoiceCode     = 300
	UnauthorizedCode       = 401
	PreconditionFailedCode = 412
	InternalErrorCode      = 500
)

// hostHTTPResult is used to save result of an Adapter's sendRequest(...) function
// it is the element of the adapter pool's channel
type hostHTTPResult struct {
	status     resultStatus
	statusCode int
	host       string
	content    string
	err        error // This is set if the http response with a status code that is not 2XX
}

type httpsResponseStatus struct {
	StatusCode int `json:"status"`
}

const respSuccStatusCode = 0

// The HTTP response with a 401 status code can have several scenarios:
// 1. Wrong password
// 2. Wrong certificate
// HTTPCheckDBRunningOp in create_db and HTTPSPollNodeStateOp in start_db need to handle these scenarios
func (hostResult *hostHTTPResult) isUnauthorizedRequest() bool {
	return hostResult.statusCode == UnauthorizedCode
}

// The HTTP response with a 412 may happen if
// the local node has not yet joined the cluster; the HTTP server will accept connections once the node joins the cluster.
func (hostResult *hostHTTPResult) hasPreconditionFailed() bool {
	return hostResult.statusCode == PreconditionFailedCode
}

// isSuccess returns true if status code is 200
func (hostResult *hostHTTPResult) isSuccess() bool {
	return hostResult.statusCode == SuccessCode
}

// check only password and certificate for start_db
func (hostResult *hostHTTPResult) isPasswordAndCertificateError(logger vlog.Printer) bool {
	if !hostResult.isUnauthorizedRequest() {
		return false
	}
	resultString := fmt.Sprintf("%v", hostResult)
	for _, msg := range wrongCredentialErrMsg {
		if strings.Contains(resultString, msg) {
			logger.Error(errors.New(msg), "the user has provided")
			return true
		}
	}
	return false
}

func (hostResult *hostHTTPResult) isInternalError() bool {
	return hostResult.statusCode == InternalErrorCode
}

func (hostResult *hostHTTPResult) isHTTPRunning() bool {
	if hostResult.isPassing() || hostResult.isUnauthorizedRequest() ||
		hostResult.isInternalError() || hostResult.hasPreconditionFailed() {
		return true
	}
	return false
}

// HTTP status code >= 200 and < 300
func (hostResult *hostHTTPResult) isPassing() bool {
	return hostResult.err == nil
}

func (hostResult *hostHTTPResult) isFailing() bool {
	return hostResult.status == FAILURE
}

func (hostResult *hostHTTPResult) isException() bool {
	return hostResult.status == EXCEPTION
}

func (hostResult *hostHTTPResult) isTimeout() bool {
	if hostResult.err != nil {
		var netErr net.Error
		if errors.As(hostResult.err, &netErr) && netErr.Timeout() {
			return true
		}
	}
	return false
}

func (hostResult *hostHTTPResult) isEOF() bool {
	return hostResult.status == EOFEXCEPTION
}

// process a single result, return the error in the result
func (hostResult *hostHTTPResult) getError(host, opName string) error {
	if hostResult.isUnauthorizedRequest() {
		return fmt.Errorf("[%s] wrong password/certificates for https service on host %s", opName, host)
	}
	if !hostResult.isPassing() {
		return hostResult.err
	}
	return nil
}

// getStatusString converts ResultStatus to string
func (status resultStatus) getStatusString() string {
	if status == FAILURE {
		return FailureResult
	} else if status == EXCEPTION {
		return ExceptionResult
	} else if status == EOFEXCEPTION {
		return EOFExceptionResult
	}
	return SuccessResult
}

/* Cluster ops interface
 */

// clusterOp interface requires that all ops implements
// the following functions
// log* implemented by embedding OpBase, but overrideable
type clusterOp interface {
	getName() string
	setLogger(logger vlog.Printer)
	setupSpinner()
	startSpinner()
	cleanupSpinner()
	stopFailSpinner()
	stopFailSpinnerWithMessage(errMsg string, v ...any)
	prepare(execContext *opEngineExecContext) error
	execute(execContext *opEngineExecContext) error
	finalize(execContext *opEngineExecContext) error
	processResult(execContext *opEngineExecContext) error
	logResponse(host string, result hostHTTPResult)
	logPrepare()
	logExecute()
	logFinalize()
	setupBasicInfo()
	applyTLSOptions(tlsOptions opTLSOptions) error
	isSkipExecute() bool
	filterUnreachableHosts(execContext *opEngineExecContext)
	filterHostsBySandbox(execContext *opEngineExecContext)
}

/* Cluster ops basic fields and functions
 */

// opBase defines base fields and implements basic functions
// for all ops
type opBase struct {
	logger             vlog.Printer
	name               string
	description        string
	hosts              []string
	clusterHTTPRequest clusterHTTPRequest
	skipExecute        bool // This can be set during prepare if we determine no work is needed
	spinner            *yacspin.Spinner
}

type opResponseMap map[string]string

func (op *opBase) getName() string {
	return op.name
}

func (op *opBase) setLogger(logger vlog.Printer) {
	op.logger = logger.WithName(op.name)
}

func (op *opBase) parseAndCheckResponse(host, responseContent string, responseObj any) error {
	err := util.GetJSONLogErrors(responseContent, &responseObj, op.name, op.logger)
	if err != nil {
		op.logger.Error(err, "fail to parse response on host, detail", "host", host, "original responseContent", responseContent)
		return err
	}
	op.logger.Info("JSON response", "host", host, "responseObj", responseObj)
	return nil
}

func (op *opBase) parseAndCheckMapResponse(host, responseContent string) (opResponseMap, error) {
	var responseObj opResponseMap
	err := op.parseAndCheckResponse(host, responseContent, &responseObj)

	return responseObj, err
}

func (op *opBase) parseAndCheckStringResponse(host, responseContent string) (string, error) {
	var responseStr string
	err := op.parseAndCheckResponse(host, responseContent, &responseStr)

	return responseStr, err
}

func (op *opBase) parseAndCheckGenericJSONResponse(host, responseContent string) (nmaGenericJSONResponse, error) {
	var genericResponse nmaGenericJSONResponse
	err := op.parseAndCheckResponse(host, responseContent, &genericResponse)

	return genericResponse, err
}

func (op *opBase) setClusterHTTPRequestName() {
	op.clusterHTTPRequest.Name = op.name
}

func (op *opBase) setVersionToSemVar() {
	op.clusterHTTPRequest.SemVar = semVer{Ver: "1.0.0"}
}

func (op *opBase) setupBasicInfo() {
	op.clusterHTTPRequest = clusterHTTPRequest{}
	op.clusterHTTPRequest.RequestCollection = make(map[string]hostHTTPRequest)
	op.setClusterHTTPRequestName()
	op.setVersionToSemVar()
}

// setupSpinner sets up the progress spinner
func (op *opBase) setupSpinner() {
	if op.logger.ForCli {
		cfg := yacspin.Config{
			Frequency:         100 * time.Millisecond,
			CharSet:           yacspin.CharSets[11],
			Suffix:            " " + op.description,
			SuffixAutoColon:   true,
			Message:           "in progress",
			StopCharacter:     "✔",
			StopColors:        []string{"fgGreen"},
			StopFailCharacter: "✘",
			StopFailMessage:   "failed",
			StopFailColors:    []string{"fgRed"},
			Writer:            op.logger.Writer, // if nil, writing to stdout
		}
		spinner, err := yacspin.New(cfg)
		if err != nil {
			op.logger.PrintWarning("[UI][%s] progress spinner failed to initialize: %v", op.name, err)
			return
		}
		spinner.Reverse()
		op.spinner = spinner
	}
}

func (op *opBase) startSpinner() {
	if op.spinner != nil {
		err := op.spinner.Start()
		if err != nil {
			op.logger.PrintWarning("[UI][%s] progress spinner failed to start: %v\n", op.name, err)
		}
	}
}

func (op *opBase) cleanupSpinner() {
	if op.spinner != nil && op.spinner.Status() == yacspin.SpinnerRunning {
		err := op.spinner.Stop()
		if err != nil {
			op.logger.PrintWarning("[UI][%s] progress spinner failed to stop: %v\n", op.name, err)
		}
	}
}

func (op *opBase) updateSpinnerMessage(msg string, v ...any) {
	if op.spinner != nil {
		op.spinner.Message(fmt.Sprintf(msg, v...))
	}
}

func (op *opBase) updateSpinnerStopMessage(msg string, v ...any) {
	if op.spinner != nil {
		op.spinner.StopMessage(fmt.Sprintf(msg, v...))
	}
}

func (op *opBase) updateSpinnerStopFailMessage(msg string, v ...any) {
	if op.spinner != nil {
		op.spinner.StopFailMessage(fmt.Sprintf(msg, v...))
	}
}

func (op *opBase) stopFailSpinner() {
	if op.spinner != nil {
		err := op.spinner.StopFail()
		if err != nil {
			op.logger.PrintWarning("Spinner error: %v", err)
		}
	}
}

func (op *opBase) stopFailSpinnerWithMessage(errMsg string, v ...any) {
	if op.spinner != nil {
		op.spinner.StopFailMessage(fmt.Sprintf(errMsg, v...))
		op.stopFailSpinner()
	}
}

func (op *opBase) logResponse(host string, result hostHTTPResult) {
	if result.err != nil {
		op.logger.PrintError("[%s] result from host %s summary %s, details: %+v",
			op.name, host, result.status.getStatusString(), result.err)
	} else {
		op.logger.Log.Info("Request succeeded",
			"op name", op.name, "host", host, "details", result)
	}
}

func (op *opBase) logPrepare() {
	op.logger.Info("Prepare() called", "name", op.name)
}

func (op *opBase) logExecute() {
	op.logger.Info("Execute() called", "name", op.name)
}

func (op *opBase) logFinalize() {
	op.logger.Info("Finalize() called", "name", op.name)
}

func (op *opBase) runExecute(execContext *opEngineExecContext) error {
	err := execContext.dispatcher.sendRequest(&op.clusterHTTPRequest, op.spinner)
	if err != nil {
		op.logger.Error(err, "Fail to dispatch request, detail", "dispatch request", op.clusterHTTPRequest)
		return err
	}
	return nil
}

type opTLSOptions interface {
	hasCerts() bool
	getCerts() *httpsCerts
	getTLSModes() *tlsModes
}

// applyTLSOptions processes TLS options here, like in-memory certificates or TLS modes,
// rather than plumbing them through to every op.
func (op *opBase) applyTLSOptions(tlsOptions opTLSOptions) error {
	if tlsOptions == nil {
		return nil
	}

	// this step is executed after Prepare() so all http requests should be set up
	if len(op.clusterHTTPRequest.RequestCollection) == 0 {
		return fmt.Errorf("[%s] clusterHTTPRequest.RequestCollection is empty", op.name)
	}

	// retrieve certs once to avoid extra copies
	var certs *httpsCerts
	if tlsOptions.hasCerts() {
		certs = tlsOptions.getCerts()
		if certs == nil {
			return fmt.Errorf("[%s] is trying to use certificates, but none are set", op.name)
		}
	}

	// always retrieve TLS modes
	tlsModes := tlsOptions.getTLSModes()
	if tlsModes == nil {
		return fmt.Errorf("[%s] unable to retrieve TLS modes from interface", op.name)
	}

	// modify requests with TLS options
	for host := range op.clusterHTTPRequest.RequestCollection {
		request := op.clusterHTTPRequest.RequestCollection[host]
		request.setCerts(certs)
		request.setTLSMode(tlsModes)
		op.clusterHTTPRequest.RequestCollection[host] = request
	}
	return nil
}

// isSkipExecute will check state to see if the Execute() portion of the
// operation should be skipped. Some operations can choose to implement this if
// they can only determine at runtime where the operation is needed. One
// instance of this is the nma_upload_config.go. If all nodes already have the
// latest catalog information, there is nothing to be done during execution.
func (op *opBase) isSkipExecute() bool {
	return op.skipExecute
}

// hasQuorum checks if we have enough working primary nodes to maintain data integrity
// quorumCount = (1/2 * number of primary nodes) + 1
func (op *opBase) hasQuorum(hostCount, primaryNodeCount uint) bool {
	quorumCount := primaryNodeCount/2 + 1
	if hostCount < quorumCount {
		op.logger.PrintError("[%s] Quorum check failed: "+
			"number of hosts with latest catalog (%d) is not "+
			"greater than 1/2 of number of the primary nodes (%d)\n",
			op.name, hostCount, primaryNodeCount)
		return false
	}

	return true
}

// checkResponseStatusCode will verify if the status code in https response is a successful code
func (op *opBase) checkResponseStatusCode(resp httpsResponseStatus, host string) (err error) {
	if resp.StatusCode != respSuccStatusCode {
		err = fmt.Errorf(`[%s] fail to execute HTTPS request on host %s, status code in HTTPS response is %d`, op.name, host, resp.StatusCode)
		op.logger.Error(err, "fail to execute HTTPS request, detail")
		return err
	}
	return nil
}

// filterUnreachableHosts filters out the unreachable hosts from the op
// if the unreachableHosts list size > 0
func (op *opBase) filterUnreachableHosts(execContext *opEngineExecContext) {
	if len(execContext.unreachableHosts) == 0 {
		return
	}

	op.hosts = util.SliceDiff(op.hosts, execContext.unreachableHosts)
}

// filterHostsBySandbox selects hosts only in the target sandbox or main cluster
func (op *opBase) filterHostsBySandbox(execContext *opEngineExecContext) {
	if execContext.sandbox == util.MainClusterSandbox {
		return
	}

	// If vdb is given as nil or the vdb is obtained,
	// we will not filter hosts by sandbox.
	// Instead, we will send requests to all hosts.
	if execContext.vdbForSandboxInfo == nil {
		return
	}

	// filter out hosts that are not in the target sandbox or main cluster
	var hostsNotInSandbox []string
	for h, vnode := range execContext.vdbForSandboxInfo.HostNodeMap {
		if vnode.Sandbox != execContext.sandbox {
			hostsNotInSandbox = append(hostsNotInSandbox, h)
		}
	}
	op.hosts = util.SliceDiff(op.hosts, hostsNotInSandbox)
}

/* Sensitive fields in request body
 */
type sensitiveFields struct {
	DBPassword         string            `json:"db_password"`
	AWSAccessKeyID     string            `json:"aws_access_key_id"`
	AWSSecretAccessKey string            `json:"aws_secret_access_key"`
	Parameters         map[string]string `json:"parameters"`
}

func (maskedData *sensitiveFields) maskSensitiveInfo() {
	const maskedValue = "******"
	sensitiveKeyParams := map[string]bool{
		"awsauth":                 true,
		"awssessiontoken":         true,
		"gcsauth":                 true,
		"azurestoragecredentials": true,
	}
	maskedData.DBPassword = maskedValue
	maskedData.AWSAccessKeyID = maskedValue
	maskedData.AWSSecretAccessKey = maskedValue
	for key := range maskedData.Parameters {
		// Mask the value if the keys are credentials
		keyLowerCase := strings.ToLower(key)
		if sensitiveKeyParams[keyLowerCase] {
			maskedData.Parameters[key] = maskedValue
		}
	}
}

/* Cluster HTTPS ops basic fields
 * which are needed for https requests using password auth
 * specify whether to use password auth explicitly
 * for the case where users do not specify a password, e.g., create db
 * we need the empty password "" string
 */
type opHTTPSBase struct {
	useHTTPPassword bool
	httpsPassword   *string
	userName        string
}

// we may add some common functions for OpHTTPSBase here

func (opb *opHTTPSBase) validateAndSetUsernameAndPassword(opName string, useHTTPPassword bool,
	userName string, httpsPassword *string) error {
	opb.useHTTPPassword = useHTTPPassword
	if opb.useHTTPPassword {
		err := util.ValidateUsernameAndPassword(opName, opb.useHTTPPassword, userName)
		if err != nil {
			return err
		}
		opb.userName = userName
		opb.httpsPassword = httpsPassword
	}

	return nil
}

type ClusterCommands interface {
	GetLog() vlog.Printer
	V(int) logr.Logger
	LogInfo(msg string, keysAndValues ...any)
	LogError(err error, msg string, keysAndValues ...any)
	PrintInfo(msg string, v ...any)
	PrintWarning(msg string, v ...any)
	PrintError(msg string, v ...any)
	DisplayInfo(msg string, v ...any)
	DisplayWarning(msg string, v ...any)
	DisplayError(msg string, v ...any)

	VAddNode(options *VAddNodeOptions) (VCoordinationDatabase, error)
	VAddSubcluster(options *VAddSubclusterOptions) error
	VAlterSubclusterType(options *VAlterSubclusterTypeOptions) error
	VCheckVClusterServerPid(options *VCheckVClusterServerPidOptions) ([]string, error)
	VCreateDatabase(options *VCreateDatabaseOptions) (VCoordinationDatabase, error)
	VCreateArchive(options *VCreateArchiveOptions) error
	VDropDatabase(options *VDropDatabaseOptions) error
	VFetchCoordinationDatabase(options *VFetchCoordinationDatabaseOptions) (VCoordinationDatabase, error)
	VFetchNodesDetails(options *VFetchNodesDetailsOptions) (NodesDetails, error)
	VFetchNodeState(options *VFetchNodeStateOptions) ([]NodeInfo, error)
	VGetDrainingStatus(options *VGetDrainingStatusOptions) (DrainingStatusList, error)
	VInstallPackages(options *VInstallPackagesOptions) (*InstallPackageStatus, error)
	VPollSubclusterState(options *VPollSubclusterStateOptions) error
	VPromoteSandboxToMain(options *VPromoteSandboxToMainOptions) error
	VReIP(options *VReIPOptions) error
	VRemoveNode(options *VRemoveNodeOptions) (VCoordinationDatabase, error)
	VRemoveSubcluster(removeScOpt *VRemoveScOptions) (VCoordinationDatabase, error)
	VRenameSubcluster(options *VRenameSubclusterOptions) error
	VReplicateDatabase(options *VReplicationDatabaseOptions) (int64, error)
	VReplicationStatus(options *VReplicationStatusDatabaseOptions) (*ReplicationStatusResponse, error)
	VReviveDatabase(options *VReviveDatabaseOptions) (dbInfo string, vdbPtr *VCoordinationDatabase, err error)
	VSandbox(options *VSandboxOptions) error
	VScrutinize(options *VScrutinizeOptions) error
	VShowRestorePoints(options *VShowRestorePointsOptions) (restorePoints []RestorePoint, err error)
	VSaveRestorePoint(options *VSaveRestorePointOptions) (err error)
	VStartDatabase(options *VStartDatabaseOptions) (vdbPtr *VCoordinationDatabase, err error)
	VStartNodes(options *VStartNodesOptions) error
	VStartSubcluster(startScOpt *VStartScOptions) (VCoordinationDatabase, error)
	VStopDatabase(options *VStopDatabaseOptions) error
	VStopNode(options *VStopNodeOptions) error
	VStopSubcluster(options *VStopSubclusterOptions) error
	VUnsandbox(options *VUnsandboxOptions) error
	VUpgradeLicense(options *VUpgradeLicenseOptions) error
}

type VClusterCommandsLogger struct {
	Log vlog.Printer
}

func (vcc VClusterCommandsLogger) GetLog() vlog.Printer {
	return vcc.Log
}

func (vcc VClusterCommandsLogger) V(level int) logr.Logger {
	return vcc.Log.V(level)
}

func (vcc VClusterCommandsLogger) LogInfo(msg string, keysAndValues ...any) {
	vcc.Log.Info(msg, keysAndValues...)
}

func (vcc VClusterCommandsLogger) LogError(err error, msg string, keysAndValues ...any) {
	vcc.Log.Error(err, msg, keysAndValues...)
}

func (vcc VClusterCommandsLogger) PrintInfo(msg string, v ...any) {
	vcc.Log.PrintInfo(msg, v...)
}

func (vcc VClusterCommandsLogger) PrintWarning(msg string, v ...any) {
	vcc.Log.PrintWarning(msg, v...)
}

func (vcc VClusterCommandsLogger) PrintError(msg string, v ...any) {
	vcc.Log.PrintError(msg, v...)
}

func (vcc VClusterCommandsLogger) DisplayInfo(msg string, v ...any) {
	vcc.Log.DisplayInfo(msg, v...)
}

func (vcc VClusterCommandsLogger) DisplayWarning(msg string, v ...any) {
	vcc.Log.DisplayWarning(msg, v...)
}

func (vcc VClusterCommandsLogger) DisplayError(msg string, v ...any) {
	vcc.Log.DisplayError(msg, v...)
}

// VClusterCommands passes state around for all top-level administrator commands
// (e.g. create db, add node, etc.).
type VClusterCommands struct {
	VClusterCommandsLogger
}
