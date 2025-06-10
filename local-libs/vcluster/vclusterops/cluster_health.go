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

package vclusterops

import (
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// VClusterHealthOptions represents the available options to check the cluster health
type VClusterHealthOptions struct {
	DatabaseOptions
	Operation          string
	TxnID              string
	NodeName           string
	StartTime          string
	EndTime            string
	SessionID          string
	Threadhold         string
	ThreadID           string
	PhaseDurationDesc  string
	EventDesc          string
	UserName           string
	Display            bool
	Timezone           string
	NeedSessionTnxInfo bool
	// duration threshold for slow events
	MinSlowDuration string
	// duration threshold for lock attempt
	LockAttemptThresHold string
	// duration threshold for lock release
	LockReleaseThresHold string

	// hidden option
	SlowEventCascade        []SlowEventNode
	SessionStartsResult     *[]dcSessionStarts
	TransactionStartsResult *[]dcTransactionStarts
	SlowEventsResult        *[]dcSlowEvent
	LockEventCascade        []NodeLockEvents
}

const (
	DefaultLockAttemptThresHold = "00:00:05" // 5 second
	DefaultLockReleaseThresHold = "00:00:05" // 5 second
	DefaultMinSlowDuration      = "1000000"  // 1 second
)

const (
	timeLayout       = "2006-01-02 15:04:05.999999"
	lockCascade      = "lock_cascade"
	slowEventCascade = "slow_event_cascade"
	getTxnStarts     = "get_transaction_starts"
	getSessionStarts = "get_session_starts"
	getSlowEvents    = "get_slow_events"
	// SlowDurationEnv is the environment variable for configuring slow duration threshold
	SlowDurationEnv = "VCLUSTER_GCLX_SLOW_DURATION"

	// LockAttemptDurationEnv is the environment variable for configuring lock attempt duration
	LockAttemptDurationEnv = "VCLUSTER_GCLX_LOCK_DURATION"
	// LockReleaseDurationEnv is the environment variable for configuring lock release duration
	LockReleaseDurationEnv = "VCLUSTER_GCLX_LOCK_RELEASE_DURATION"
)

// VClusterHealthFactory creates and returns a VClusterHealthOptions instance with default values set.
func VClusterHealthFactory() VClusterHealthOptions {
	options := VClusterHealthOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (opt *VClusterHealthOptions) setDefaultValues() {
	opt.DatabaseOptions.setDefaultValues()
	if opt.MinSlowDuration == "" {
		opt.MinSlowDuration = DefaultMinSlowDuration
	}
	if opt.LockAttemptThresHold == "" {
		opt.LockAttemptThresHold = DefaultLockAttemptThresHold
	}
	if opt.LockReleaseThresHold == "" {
		opt.LockReleaseThresHold = DefaultLockReleaseThresHold
	}
}

func (opt *VClusterHealthOptions) validateRequiredOptions(logger vlog.Printer) error {
	err := opt.validateBaseOptions(ClusterHealthCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (opt *VClusterHealthOptions) validateParseOptions(logger vlog.Printer) error {
	return opt.validateRequiredOptions(logger)
}

func (opt *VClusterHealthOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(opt.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		opt.Hosts, err = util.ResolveRawHostsToAddresses(opt.RawHosts, opt.IPv6)
		if err != nil {
			return err
		}
		opt.normalizePaths()
	}

	// analyze start and end time
	if opt.Timezone != "" {
		err := opt.convertDateStringToUTC()
		if err != nil {
			return err
		}
	}

	return nil
}

func (opt *VClusterHealthOptions) convertDateStringToUTC() error {
	// convert start time to UTC
	if opt.StartTime != "" {
		startTime, err := util.ConvertDateStringToUTC(opt.StartTime, opt.Timezone)
		if err != nil {
			return err
		}
		opt.StartTime = startTime
	}

	// convert end time to UTC
	if opt.EndTime != "" {
		endTime, err := util.ConvertDateStringToUTC(opt.EndTime, opt.Timezone)
		if err != nil {
			return err
		}
		opt.EndTime = endTime
	}

	return nil
}

func (opt *VClusterHealthOptions) validateAnalyzeOptions(log vlog.Printer) error {
	if err := opt.validateParseOptions(log); err != nil {
		return err
	}
	err := opt.analyzeOptions()
	if err != nil {
		return err
	}
	return opt.setUsePasswordAndValidateUsernameIfNeeded(log)
}

func (vcc VClusterCommands) VClusterHealth(options *VClusterHealthOptions) error {
	// need username for Go client authentication
	err := options.validateUserName(vcc.Log)
	if err != nil {
		return err
	}

	vdb := makeVCoordinationDatabase()

	// validate and analyze options
	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	err = vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, util.MainClusterSandbox)
	if err != nil {
		return err
	}

	// if the up nodes are not healthy, we can early fail out
	err = options.checkNMAHealth(vcc.Log, vdb.PrimaryUpNodes)
	if err != nil {
		return err
	}

	var runError error
	switch options.Operation {
	case getSlowEvents:
		options.SlowEventsResult, runError = options.getSlowEvents(vcc.Log, vdb.PrimaryUpNodes, options.ThreadID, options.StartTime,
			options.EndTime)
	case getSessionStarts:
		options.SessionStartsResult, runError = options.getSessionStarts(vcc.Log, vdb.PrimaryUpNodes, options.SessionID)
	case getTxnStarts:
		options.TransactionStartsResult, runError = options.getTransactionStarts(vcc.Log, vdb.PrimaryUpNodes, options.TxnID)
	case slowEventCascade:
		options.NeedSessionTnxInfo = true
		runError = options.buildCascadeGraph(vcc.Log, vdb.PrimaryUpNodes)
	case lockCascade:
		runError = options.buildLockCascadeGraph(vcc.Log, vdb.PrimaryUpNodes)
	default: // by default, we will build a cascade graph
		runError = options.buildCascadeGraph(vcc.Log, vdb.PrimaryUpNodes)
	}

	return runError
}

func (opt *VClusterHealthOptions) checkNMAHealth(logger vlog.Printer, upHosts []string) error {
	var instructions []clusterOp

	nmaHealthOp := makeNMAHealthOp(upHosts)
	instructions = append(instructions, &nmaHealthOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &opt.DatabaseOptions)

	return clusterOpEngine.run(logger)
}

func (opt *VClusterHealthOptions) getSlowEvents(logger vlog.Printer, upHosts []string,
	threadID, startTime, endTime string) (slowEvents *[]dcSlowEvent, err error) {
	var instructions []clusterOp

	nmaSlowEventOp, err := makeNMASlowEventOp(upHosts, opt.DatabaseOptions.UserName,
		opt.DatabaseOptions.DBName, opt.DatabaseOptions.Password,
		startTime, endTime, threadID, opt.PhaseDurationDesc,
		opt.TxnID, opt.EventDesc, opt.NodeName, opt.MinSlowDuration)
	if err != nil {
		return nil, err
	}
	instructions = append(instructions, &nmaSlowEventOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &opt.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return slowEvents, fmt.Errorf("fail to get slow events, %w", err)
	}
	return clusterOpEngine.execContext.dcSlowEventList, nil
}

func (opt *VClusterHealthOptions) getSessionStarts(logger vlog.Printer, upHosts []string,
	sessionID string) (sessionStartList *[]dcSessionStarts, err error) {
	var instructions []clusterOp

	nmaSessionStartsOp, err := makeNMASessionStartsOp(upHosts, opt.DatabaseOptions.UserName,
		opt.DatabaseOptions.DBName, opt.DatabaseOptions.Password, sessionID,
		opt.StartTime, opt.EndTime)
	if err != nil {
		return nil, err
	}
	instructions = append(instructions, &nmaSessionStartsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &opt.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return sessionStartList, fmt.Errorf("fail to get session Starts, %w", err)
	}
	return clusterOpEngine.execContext.dcSessionStarts, nil
}

func (opt *VClusterHealthOptions) getTransactionStarts(logger vlog.Printer, upHosts []string,
	txnID string) (transactionInfo *[]dcTransactionStarts, err error) {
	var instructions []clusterOp

	nmaTransactionStartsOp, err := makeNMATransactionStartsOp(upHosts, opt.DatabaseOptions.UserName,
		opt.DatabaseOptions.DBName, opt.DatabaseOptions.Password, txnID,
		opt.StartTime, opt.EndTime)
	if err != nil {
		return nil, fmt.Errorf("fail to construct transaction starts op, %w", err)
	}
	instructions = append(instructions, &nmaTransactionStartsOp)

	clusterOpEngine := makeClusterOpEngine(instructions, &opt.DatabaseOptions)
	err = clusterOpEngine.run(logger)
	if err != nil {
		return transactionInfo, fmt.Errorf("fail to get transaction starts, %w", err)
	}

	return clusterOpEngine.execContext.dcTransactionStarts, nil
}
