/*
 (c) Copyright [2024] Open Text.
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
	"strings"
)

type nmaSignalVerticaOp struct {
	opBase
	signal         string            // signal type ("term" or "kill" or "" for endpoint default)
	hostCatPathMap map[string]string // map of hosts to catalog paths. may be nil to allow backend to auto-detect vertica pids.
}

func makeNMASignalVerticaOpHelper(hosts []string, hostCatPathMap map[string]string) (op nmaSignalVerticaOp, err error) {
	op = nmaSignalVerticaOp{}
	op.name = "NMASignalVerticaOp"
	op.description = "Terminate applicable nodes via signal"
	op.hosts = hosts
	op.hostCatPathMap = hostCatPathMap
	if op.hostCatPathMap != nil {
		// the caller is responsible for making sure hosts and maps match up exactly
		err = validateHostMaps(hosts, hostCatPathMap)
	}
	return op, err
}

func makeNMASigTermVerticaOp(hosts []string, hostCatPathMap map[string]string) (op nmaSignalVerticaOp, err error) {
	op, err = makeNMASignalVerticaOpHelper(hosts, hostCatPathMap)
	op.signal = "term"
	return op, err
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaSignalVerticaOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("vertica-processes/signal")

		// signal vertica endpoint uses query params despite being POST
		httpRequest.QueryParams = map[string]string{"signal_type": op.signal}

		// Passing the catalog path allows the backend to find the vertica pid directly.
		// The catalog path for the signal endpoint is the parent dir containing pid, log, etc.
		// If we can't figure out the dir, rely on auto-detecting processes by skipping the arg.
		if op.hostCatPathMap != nil && strings.HasSuffix(op.hostCatPathMap[host], "/Catalog") {
			httpRequest.QueryParams["catalog_path"] = strings.TrimSuffix(op.hostCatPathMap[host], "/Catalog")
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaSignalVerticaOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaSignalVerticaOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaSignalVerticaOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaSignalVerticaOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	var errorHosts []string
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		var err error

		if result.isPassing() {
			_, err = op.parseAndCheckMapResponse(host, result.content)
			if err == nil {
				// Note that a passing result means the signal was sent successfully, but
				// not that the process has successfully terminated yet (or at all).
				continue
			}
		} else {
			err = result.err
		}
		errorHosts = append(errorHosts, host)
		allErrs = errors.Join(allErrs, err)
	}

	if allErrs != nil {
		err := fmt.Errorf("Error terminating Vertica via signal on hosts %v", errorHosts)
		allErrs = errors.Join(err, allErrs)
	}

	return allErrs
}
