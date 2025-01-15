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
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

type httpsStageSystemTablesOp struct {
	opBase
	opHTTPSBase
	id              string
	hostNodeNameMap map[string]string
	stagingDir      *string
	excludedTables  []string
	tlsOptions      opTLSOptions // for resetting certs on each new request set
	timeoutError    error        // for breaking out early if systable gathering times out
}

type prepareStagingSystemTableRequestData struct {
	StagingDirectory string            `json:"staging_directory"`
	SystemTableList  []systemTableInfo `json:"system_table_list"`
}

func (*httpsStageSystemTablesOp) getNormalExcludeTables() []string {
	return []string{
		"vs_ros_min_max_values",
		"vs_projection_column_histogram",
		"vs_passwords",
		"vs_passwords_helper",
		"cryptographic_keys",
		"certificates",
		"vs_new_storage_container_columns",
		"vs_storage_reference_counts",
		"vs_bundled_ros",
	}
}

func (*httpsStageSystemTablesOp) getContainersExcludeTables() []string {
	return []string{
		"vs_partitions",
		"storage_containers",
		"vs_column_storage",
		"vs_storage_columns",
		"vs_strata",
		"vs_ros_segment_bounds",
		"delete_vectors",
		"vs_segments",
		"vs_projection_segment_information",
	}
}

func (*httpsStageSystemTablesOp) getActiveQueriesExcludeTables() []string {
	return []string{
		"vs_execution_engine_profiles",
	}
}

func (*httpsStageSystemTablesOp) getRosTables() []string {
	return []string{
		"vs_ros",
		"vs_ros_containers",
	}
}

func (*httpsStageSystemTablesOp) getExternalTableDetailsTables() []string {
	return []string{
		"external_table_details",
	}
}

func (*httpsStageSystemTablesOp) getUDXDetailsTables() []string {
	return []string{
		"user_library_manifest", // VER-92401: lazy-loading UDXs on Eon can take 20+ minutes
	}
}

func generateExcludedTableList(
	excludeContainers bool,
	excludeActiveQueries bool,
	includeRos bool,
	includeExternalTableDetails bool,
	includeUDXDetails bool,
	op *httpsStageSystemTablesOp,
) (excludedTables []string) {
	excludedTables = op.getNormalExcludeTables()
	if excludeContainers {
		excludedTables = append(excludedTables, op.getContainersExcludeTables()...)
	}
	if excludeActiveQueries {
		excludedTables = append(excludedTables, op.getActiveQueriesExcludeTables()...)
	}
	if !includeRos {
		excludedTables = append(excludedTables, op.getRosTables()...)
	}
	if !includeExternalTableDetails {
		excludedTables = append(excludedTables, op.getExternalTableDetailsTables()...)
	}
	if !includeUDXDetails {
		excludedTables = append(excludedTables, op.getUDXDetailsTables()...)
	}
	return excludedTables
}

func makeHTTPSStageSystemTablesOp(logger vlog.Printer,
	useHTTPPassword bool, userName string, httpsPassword *string,
	id string, hostNodeNameMap map[string]string,
	stagingDir *string,
	excludeContainers bool,
	excludeActiveQueries bool,
	includeRos bool,
	includeExternalTableDetails bool,
	includeUDXDetails bool,
) (httpsStageSystemTablesOp, error) {
	op := httpsStageSystemTablesOp{}
	op.name = "HTTPSStageSystemTablesOp"
	op.description = "Stage system tables"
	op.logger = logger.WithName(op.name)
	op.hosts = maps.Keys(hostNodeNameMap)
	op.useHTTPPassword = useHTTPPassword
	op.id = id
	op.hostNodeNameMap = hostNodeNameMap
	op.stagingDir = stagingDir
	op.excludedTables = generateExcludedTableList(excludeContainers,
		excludeActiveQueries,
		includeRos,
		includeExternalTableDetails,
		includeUDXDetails,
		&op)

	if useHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
		if err != nil {
			return op, err
		}
		op.userName = userName
		op.httpsPassword = httpsPassword
	}

	// define this error here for scoping
	op.timeoutError = errors.New("timed out during system table staging")

	return op, nil
}

func (op *httpsStageSystemTablesOp) setupClusterHTTPRequest(hosts []string, schema, tableName string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint("system-tables/stage")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		systemTable := systemTableInfo{}
		systemTable.Schema = schema
		systemTable.TableName = tableName
		requestData := prepareStagingSystemTableRequestData{}
		requestData.StagingDirectory = *op.stagingDir
		requestData.SystemTableList = []systemTableInfo{systemTable}

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}
		httpRequest.RequestData = string(dataBytes)

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsStageSystemTablesOp) prepare(execContext *opEngineExecContext) error {
	host := getInitiatorFromUpHosts(execContext.upHosts, op.hosts)
	if host == "" {
		op.logger.PrintWarning("no up hosts among user specified hosts to collect system tables from, skipping the operation")
		op.skipExecute = true
		return nil
	}

	// construct host list for interface purposes
	op.hosts = []string{host}

	execContext.dispatcher.setup(op.hosts)
	return nil
}

func (op *httpsStageSystemTablesOp) execute(execContext *opEngineExecContext) error {
	for _, systemTableInfo := range execContext.systemTableList.SystemTableList {
		if slices.Contains(op.excludedTables, systemTableInfo.TableName) {
			continue
		}
		if err := op.setupClusterHTTPRequest(op.hosts, systemTableInfo.Schema, systemTableInfo.TableName); err != nil {
			return err
		}
		if err := op.opBase.applyTLSOptions(op.tlsOptions); err != nil {
			return err
		}
		op.logger.Info("Staging System Table:", "Schema", systemTableInfo.Schema, "Table", systemTableInfo.TableName)
		if err := op.runExecute(execContext); err != nil {
			return err
		}
		if err := op.processResult(execContext); err != nil {
			// if staging a system table times out, don't take down the run,
			// but don't keep trying as the timeouts could add up to hours if
			// deterministic
			if errors.Is(err, op.timeoutError) {
				op.logger.Error(err, "Halting system table staging")
				op.logger.PrintWarning("Timed out staging table %s.%s. Skipping remaining system tables.",
					systemTableInfo.Schema, systemTableInfo.TableName)
				break
			}
			return err
		}
	}
	return nil
}

func (op *httpsStageSystemTablesOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		if result.isPassing() {
			op.logger.Info("Staging System Table Success")
		} else if result.isInternalError() {
			// staging system tables can fail for various reasons that should not fail
			// the run, e.g. if DelimitedExport is uninstalled
			op.logger.Error(result.err, "Failed to stage table")
		} else if result.isTimeout() {
			allErrs = errors.Join(allErrs, op.timeoutError, result.err)
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}

func (op *httpsStageSystemTablesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

// applyTLSOptions shadows the op base function and stashes the interface providing certificates
// and other TLS options (like modes) instead of immediately setting them, as httpsStageSystemTablesOp
// delays creation of request objects and resets them repeatedly
func (op *httpsStageSystemTablesOp) applyTLSOptions(tlsOptions opTLSOptions) error {
	op.tlsOptions = tlsOptions
	return nil
}
