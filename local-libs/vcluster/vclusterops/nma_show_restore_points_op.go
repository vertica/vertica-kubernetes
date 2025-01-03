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

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type nmaShowRestorePointsOp struct {
	opBase
	dbName                  string
	communalLocation        string
	configurationParameters map[string]string
	filterOptions           ShowRestorePointFilterOptions
}

// Optional arguments to list only restore points that
// meet the specified condition(s)
type ShowRestorePointFilterOptions struct {
	// Only list restore points with given archive name
	ArchiveName string
	// Only list restore points created no earlier than this timestamp (must be UTC timezone)
	StartTimestamp string
	// Only list restore points created no later than this timestamp (must be UTC timezone)
	EndTimestamp string
	// Only list restore points with given ID
	ArchiveID string
	// Only list restore points with given index
	ArchiveIndex string
}

type showRestorePointsRequestData struct {
	DBName           string            `json:"db_name"`
	CommunalLocation string            `json:"communal_location"`
	Parameters       map[string]string `json:"parameters,omitempty"`
	ArchiveName      string            `json:"archive_name,omitempty"`
	StartTimestamp   string            `json:"start_timestamp,omitempty"`
	EndTimestamp     string            `json:"end_timestamp,omitempty"`
	ArchiveID        string            `json:"archive_id,omitempty"`
	ArchiveIndex     string            `json:"archive_index,omitempty"`
}

// This op is used to show restore points in a database
func makeNMAShowRestorePointsOp(logger vlog.Printer,
	hosts []string, dbName, communalLocation string, configurationParameters map[string]string) nmaShowRestorePointsOp {
	return nmaShowRestorePointsOp{
		opBase: opBase{
			name:        "NMAShowRestorePointsOp",
			description: "Run restore points query",
			logger:      logger.WithName("NMAShowRestorePointsOp"),
			hosts:       hosts,
		},
		dbName:                  dbName,
		configurationParameters: configurationParameters,
		communalLocation:        communalLocation,
	}
}

// This op is used to show restore points in a database
func makeNMAShowRestorePointsOpWithFilterOptions(logger vlog.Printer,
	hosts []string, dbName, communalLocation string, configurationParameters map[string]string,
	filterOptions *ShowRestorePointFilterOptions) nmaShowRestorePointsOp {
	op := makeNMAShowRestorePointsOp(logger, hosts, dbName, communalLocation, configurationParameters)
	op.filterOptions = *filterOptions
	return op
}

// make https json data
func (op *nmaShowRestorePointsOp) setupRequestBody() (map[string]string, error) {
	hostRequestBodyMap := make(map[string]string, len(op.hosts))
	for _, host := range op.hosts {
		requestData := showRestorePointsRequestData{}
		requestData.DBName = op.dbName
		requestData.CommunalLocation = op.communalLocation
		requestData.Parameters = op.configurationParameters
		requestData.ArchiveName = op.filterOptions.ArchiveName
		requestData.StartTimestamp = op.filterOptions.StartTimestamp
		requestData.EndTimestamp = op.filterOptions.EndTimestamp
		requestData.ArchiveID = op.filterOptions.ArchiveID
		requestData.ArchiveIndex = op.filterOptions.ArchiveIndex

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return nil, fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}
		hostRequestBodyMap[host] = string(dataBytes)
	}
	return hostRequestBodyMap, nil
}

func (op *nmaShowRestorePointsOp) setupClusterHTTPRequest(hostRequestBodyMap map[string]string) error {
	for host, requestBody := range hostRequestBodyMap {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("restore-points")
		httpRequest.RequestData = requestBody
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaShowRestorePointsOp) prepare(execContext *opEngineExecContext) error {
	hostRequestBodyMap, err := op.setupRequestBody()
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(hostRequestBodyMap)
}

func (op *nmaShowRestorePointsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaShowRestorePointsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

// RestorePoint contains information about a single restore point.
type RestorePoint struct {
	// Name of the archive that this restore point was created in.
	Archive string `json:"archive,omitempty"`
	// The ID of the restore point. This is a form of a UID that is static for the restore point.
	ID string `json:"id,omitempty"`
	// The current index of this restore point. Lower value means it was taken more recently.
	// This changes when new restore points are created.
	Index int `json:"index,omitempty"`
	// The timestamp when the restore point was created.
	Timestamp string `json:"timestamp,omitempty"`
	// The version of Vertica running when the restore point was created.
	VerticaVersion string `json:"vertica_version,omitempty"`
}

/*
Sample response from the NMA restore-points endpoint:
[

	{
	    "archive": "db",
	    "id": "4ee4119b-802c-4bb4-94b0-061c8748b602",
	    "index": 1,
	    "timestamp": "2023-05-02 14:10:31.038289",
	    "vertica_version": "v24.2.0-e6bb47b39502d8f4c6f68619f4d4a4648707fd42"
	},
	{
	    "archive": "db",
	    "id": "bdaa4764-d8aa-4979-89e5-e642cc58d972",
	    "index": 2,
	    "timestamp": "2023-05-02 14:10:28.717667",
	    "vertica_version": "v24.2.0-e6bb47b39502d8f4c6f68619f4d4a4648707fd42"
	}

]
*/
func (op *nmaShowRestorePointsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			var responseObj []RestorePoint
			err := op.parseAndCheckResponse(host, result.content, &responseObj)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}
			op.logger.PrintInfo("[%s] response: %v", op.name, result.content)
			execContext.restorePoints = responseObj
			return nil
		}

		allErrs = errors.Join(allErrs, result.err)
	}
	return allErrs
}
