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
	"errors"
	"fmt"
	"os"

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaGetScrutinizeTarOp struct {
	scrutinizeOpBase
	useInitiator bool
}

func makeNMAGetScrutinizeTarOp(
	id, batch string,
	hosts []string,
	hostNodeNameMap map[string]string) (nmaGetScrutinizeTarOp, error) {
	// base members
	op := nmaGetScrutinizeTarOp{}
	op.name = "NMAGetScrutinizeTarOp"
	op.description = fmt.Sprintf("Create and retrieve tar files for batch %s", batch)
	op.hosts = hosts

	// scrutinize members
	op.id = id
	op.batch = batch
	op.hostNodeNameMap = hostNodeNameMap
	op.httpMethod = GetMethod

	// the caller is responsible for making sure hosts and maps match up exactly
	err := validateHostMaps(hosts, hostNodeNameMap)
	if err != nil {
		return op, err
	}

	err = op.createOutputDir()
	return op, err
}

// useSingleHost indicates that the tarball should only be retrieved from the first
// up node
func (op *nmaGetScrutinizeTarOp) useSingleHost() {
	op.useInitiator = true
}

// createOutputDir creates a subdirectory {id} under /tmp/scrutinize/remote, which
// may also be created by this function.  the "remote" subdirectory is created to
// separate local scrutinize data staged by the NMA (placed in /tmp/scrutinize/) from
// data gathered by vcluster from all reachable hosts.
func (op *nmaGetScrutinizeTarOp) createOutputDir() error {
	const OwnerReadWriteExecute = 0700
	outputDir := fmt.Sprintf("%s/%s/", scrutinizeRemoteOutputPath, op.id)
	if err := os.MkdirAll(outputDir, OwnerReadWriteExecute); err != nil {
		return err
	}
	stagingDirPathAccess := util.CanWriteAccessPath(outputDir)
	if stagingDirPathAccess == util.FileNotExist {
		return fmt.Errorf("opening scrutinize output directory failed: '%s'", outputDir)
	}
	if stagingDirPathAccess == util.NoWritePerm {
		return fmt.Errorf("scrutinize output directory not writeable: '%s'", outputDir)
	}
	return nil
}

func (op *nmaGetScrutinizeTarOp) prepare(execContext *opEngineExecContext) error {
	// for the system table batch
	if op.useInitiator {
		if len(execContext.upHosts) == 0 {
			op.logger.PrintWarning("no up hosts to collect system tables from, skipping the operation")
			op.skipExecute = true
			return nil
		}

		host := getInitiatorFromUpHosts(execContext.upHosts, op.hosts)
		if host == "" {
			op.logger.PrintWarning("no up hosts among user specified hosts to collect system tables from, skipping the operation")
			op.skipExecute = true
			return nil
		}

		op.hosts = []string{host}
	}

	hostToFilePathsMap := map[string]string{}
	for _, host := range op.hosts {
		hostToFilePathsMap[host] = fmt.Sprintf("%s/%s/%s-%s.tgz",
			scrutinizeRemoteOutputPath,
			op.id,
			op.hostNodeNameMap[host],
			op.batch)
	}
	execContext.dispatcher.setupForDownload(op.hosts, hostToFilePathsMap)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaGetScrutinizeTarOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaGetScrutinizeTarOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaGetScrutinizeTarOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			op.logger.Info("Retrieved tarball",
				"Host", host,
				"Node", op.hostNodeNameMap[host],
				"Batch", op.batch)
		} else {
			op.logger.Error(result.err, "Failed to retrieve tarball",
				"Host", host,
				"Node", op.hostNodeNameMap[host],
				"Batch", op.batch)
			if result.isInternalError() {
				op.logger.PrintWarning("Failed to tar batch %s on host %s. Skipping.", op.batch, host)
			} else {
				err := fmt.Errorf("failed to retrieve tarball batch %s on host %s, details %w",
					op.batch, host, result.err)
				allErrs = errors.Join(allErrs, err)
			}
		}
	}

	return allErrs
}
