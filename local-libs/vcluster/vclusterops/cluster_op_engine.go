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

type VClusterOpEngine struct {
	instructions []clusterOp
	tlsOptions   opTLSOptions
	execContext  *opEngineExecContext
}

func makeClusterOpEngine(instructions []clusterOp, tlsOptions opTLSOptions) VClusterOpEngine {
	newClusterOpEngine := VClusterOpEngine{}
	newClusterOpEngine.instructions = instructions
	newClusterOpEngine.tlsOptions = tlsOptions
	return newClusterOpEngine
}

func (opEngine *VClusterOpEngine) run(logger vlog.Printer) error {
	// when vdb is nil or sandbox is not specified,
	// the op engine will not filter any hosts to send requests
	return opEngine.runInSandbox(logger, nil /*vdb*/, util.MainClusterSandbox)
}

func (opEngine *VClusterOpEngine) runInSandbox(logger vlog.Printer,
	vdb *VCoordinationDatabase, sandbox string) error {
	execContext := makeOpEngineExecContext(logger)
	execContext.vdbForSandboxInfo = vdb
	execContext.sandbox = sandbox
	opEngine.execContext = &execContext

	return opEngine.runWithExecContext(logger, &execContext)
}

func (opEngine *VClusterOpEngine) runWithExecContext(logger vlog.Printer, execContext *opEngineExecContext) error {
	for _, op := range opEngine.instructions {
		err := opEngine.runInstruction(logger, execContext, op)
		if err != nil {
			return err
		}
	}

	// display warning if any unreachable hosts detected
	if len(opEngine.execContext.unreachableHosts) > 0 {
		logger.DisplayWarning("Unreachable host(s) detected, please check the NMA connectivity in %v",
			opEngine.execContext.unreachableHosts)
	}

	return nil
}

func (opEngine *VClusterOpEngine) runInstruction(
	logger vlog.Printer, execContext *opEngineExecContext,
	op clusterOp) error {
	op.setLogger(logger)
	op.setupBasicInfo()
	op.setupSpinner()
	defer op.cleanupSpinner()

	op.filterUnreachableHosts(execContext)
	op.filterHostsBySandbox(execContext)

	op.logPrepare()
	err := op.prepare(execContext)
	if err != nil {
		return fmt.Errorf("prepare %s failed, details: %w", op.getName(), err)
	}

	if !op.isSkipExecute() {
		// start the progress spinner
		op.startSpinner()

		err = op.applyTLSOptions(opEngine.tlsOptions)
		if err != nil {
			// here we do not return an error as the spinner error does not
			// affect the functionality
			op.stopFailSpinnerWithMessage(err.Error())
			return fmt.Errorf("applying TLS options for %s failed, details: %w", op.getName(), err)
		}

		// execute an instruction
		op.logExecute()
		err = op.execute(execContext)
		if err != nil {
			// here we do not return an error as the spinner error does not
			// affect the functionality
			op.stopFailSpinner()
			return fmt.Errorf("execute %s failed, details: %w", op.getName(), err)
		}
	}

	op.logFinalize()
	err = op.finalize(execContext)
	if err != nil {
		return fmt.Errorf("finalize %s failed, details: %w", op.getName(), err)
	}

	logger.PrintInfo("[%s] is successfully completed", op.getName())

	return nil
}
