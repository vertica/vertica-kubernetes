/*
 (c) Copyright [2023-2025] Open Text.
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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/vertica/vcluster/vclusterops/util"
)

type workloadReplayData struct {
	originalWorkloadData []workloadQuery // Data from original workload capture
	replayData           []workloadQuery // Data captured during replay
}

type nmaWorkloadReplayOp struct {
	opBase
	nmaWorkloadReplayRequestData
	hosts              []string
	hostNodeMap        vHostNodeMap
	hostRequestBodyMap map[string]string

	workloadReplayData *workloadReplayData
	JobID              int64
}

func makeNMAWorkloadReplayOp(hosts []string, usePassword bool, hostNodeMap vHostNodeMap,
	workloadReplayRequestData *nmaWorkloadReplayRequestData,
	workloadReplayData *workloadReplayData) (nmaWorkloadReplayOp, error) {
	op := nmaWorkloadReplayOp{}
	op.name = "NMAWorkloadReplayOp"
	op.description = "Replay workload"
	op.hosts = hosts
	op.hostNodeMap = hostNodeMap
	op.nmaWorkloadReplayRequestData = *workloadReplayRequestData
	op.workloadReplayData = workloadReplayData
	op.JobID = workloadReplayRequestData.JobID

	if usePassword {
		err := util.ValidateUsernameAndPassword(op.name, usePassword, workloadReplayRequestData.UserName)
		if err != nil {
			return op, err
		}
		op.UserName = workloadReplayRequestData.UserName
		op.Password = workloadReplayRequestData.Password
	}

	return op, nil
}

// Request data to be sent to NMA capture workload endpoint
type nmaWorkloadReplayRequestData struct {
	DBName   string  `json:"dbname"`
	UserName string  `json:"username"`
	Password *string `json:"password"`
	Request  string  `json:"request"`
	StmtType string  `json:"statement_type"`
	FileName string  `json:"file_name"`
	FileDir  string  `json:"file_dir"`
	JobID    int64   `json:"job_id"`
}

// Create the request body JSON string for a preprocessed workload query
func (op *nmaWorkloadReplayOp) updateRequestBody(hosts []string, query VWorkloadPreprocessResponse) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		op.nmaWorkloadReplayRequestData.Request = query.VParsedQuery
		op.nmaWorkloadReplayRequestData.StmtType = query.VStmtType
		op.nmaWorkloadReplayRequestData.FileName = query.VFileName
		op.nmaWorkloadReplayRequestData.JobID = op.JobID

		if query.VFileDir != "" {
			op.nmaWorkloadReplayRequestData.FileDir = op.hostNodeMap[host].CatalogPath
		} else {
			op.nmaWorkloadReplayRequestData.FileDir = query.VFileDir
		}

		dataBytes, err := json.Marshal(op.nmaWorkloadReplayRequestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// Set up HTTP requests to send to NMA hosts
func (op *nmaWorkloadReplayOp) setupClusterHTTPRequest(hosts []string, timeout int) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("workload-replay/replay")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		httpRequest.Timeout = timeout
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

// Validate the original workload data can actually be replayed without errors.
// No empty queries, incorrect date formatting, etc.
func (op *nmaWorkloadReplayOp) validateOriginalWorkloadData() error {
	originalData := op.workloadReplayData.originalWorkloadData

	for index, workloadQuery := range originalData {
		// Check for invalid start timestamp
		_, err := parseWorkloadTime(workloadQuery.StartTimestamp)
		if err != nil {
			return fmt.Errorf("invalid start timestamp at workload query index %d: %w", index, err)
		}

		if workloadQuery.Request == "" {
			return fmt.Errorf("empty request at index %d", index)
		}

		if workloadQuery.NodeName == "" {
			return fmt.Errorf("empty node name at index %d", index)
		}
	}

	return nil
}

func (op *nmaWorkloadReplayOp) prepare(execContext *opEngineExecContext) error {
	err := op.validateOriginalWorkloadData()
	if err != nil {
		return fmt.Errorf("invalid workload file data: %w", err)
	}

	op.workloadReplayData.replayData = []workloadQuery{}

	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts, 0)
}

// Set up HTTP request for a particular query
func (op *nmaWorkloadReplayOp) prepareRequest(originalQuery *workloadQuery, query VWorkloadPreprocessResponse) error {
	err := op.updateRequestBody(op.hosts, query)
	if err != nil {
		return err
	}

	// Queries can take a long time to complete, so increase the request timeout if necessary
	// Use either the default (5 minutes) or 2x original captured duration, whichever is higher
	timeoutMultiplier := 2.0
	originalRequestDurationTimeout := math.Floor(float64(originalQuery.RequestDurationMS) / 1000 * timeoutMultiplier)
	timeout := math.Max(defaultRequestTimeout, originalRequestDurationTimeout)

	// Convert timeout from int64 to int
	timeoutInt := 0
	if timeout <= math.MaxInt { // Max int in seconds is billions of years. This should never be false
		timeoutInt = int(timeout)
	}
	op.logger.Log.Info(fmt.Sprintf("Using timeout of %d seconds", timeoutInt), "name", op.name)

	return op.setupClusterHTTPRequest(op.hosts, timeoutInt)
}

func parseWorkloadTime(timestamp string) (time.Time, error) {
	const dateFormat = "2006-01-02T15:04:05.999999-07:00"
	parsedTime, err := time.Parse(dateFormat, timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("fail to parse workload timestamp: %w", err)
	}
	return parsedTime, nil
}

// In the event we hit an error during workload replay, append a row to the replay results containing the error detail
func (op *nmaWorkloadReplayOp) appendReplayErrorRow(err error) {
	errorRow := workloadQuery{
		ErrorDetails: err.Error(),
	}
	op.workloadReplayData.replayData = append(op.workloadReplayData.replayData, errorRow)
}

// executeWorkloadReplay is the main entry point for replaying SQL workload sessions
// It groups sessions, builds a dependency graph, performs topological sort,
// and replays sessions in parallel while honoring dependencies and timing.
func (op *nmaWorkloadReplayOp) executeWorkloadReplay(execContext *opEngineExecContext) error {
	originalData := op.workloadReplayData.originalWorkloadData
	if len(originalData) == 0 {
		return nil
	}

	sessionGroups, sessionAccessMap, sessionStartTimes := op.groupSessions(originalData)
	sessionDependencyGraph := op.buildSessionDependencies(sessionAccessMap, sessionStartTimes)
	sortedSessions, err := topologicalSort(sessionGroups, sessionDependencyGraph)
	if err != nil {
		return err
	}

	op.logger.Log.Info("Started executing workload replay with dependency awareness", "name", op.name)
	op.runReplayGraph(execContext, sessionGroups, sessionDependencyGraph, sortedSessions)
	op.logger.Log.Info("Finished executing workload replay", "name", op.name)
	return nil
}

// isWriteQuery returns true if the SQL query is a write operation (INSERT/UPDATE/DELETE/CREATE/etc)
func isWriteQuery(sql string) bool {
	writeRe := regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|MERGE|CREATE|DROP|ALTER|TRUNCATE)\b`)
	return writeRe.MatchString(sql)
}

// extractUsedTables identifies table names involved in a SQL query for dependency tracking
func extractUsedTables(sql string) []string {
	// Regex to find table names after common keywords (case-insensitive)
	tablePattern := regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE|TABLE|DELETE\s+FROM)\s+([a-zA-Z0-9_.]+)\b`)
	matches := tablePattern.FindAllStringSubmatch(sql, -1)

	// Extract table names from regex matches
	tableNames := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			tableNames = append(tableNames, match[1])
		}
	}

	return tableNames
}

// tableAccess represents whether a session reads from or writes to a given table
type tableAccess struct {
	read  bool
	write bool
}

// groupSessions organizes workload queries by session, determines table accesses, and records session start times
func (op *nmaWorkloadReplayOp) groupSessions(data []workloadQuery) (
	sessionGroups map[string][]workloadQuery,
	sessionAccessMap map[string]map[string]*tableAccess,
	sessionStartTimes map[string]time.Time,
) {
	sessionGroups = make(map[string][]workloadQuery)
	sessionAccessMap = make(map[string]map[string]*tableAccess)
	sessionStartTimes = make(map[string]time.Time)

	for _, query := range data {
		sid := query.SessionID
		sessionGroups[sid] = append(sessionGroups[sid], query)

		if sessionAccessMap[sid] == nil {
			sessionAccessMap[sid] = make(map[string]*tableAccess)
		}

		queryStart, err := parseWorkloadTime(query.StartTimestamp)
		if err != nil {
			continue
		}

		// Record the earliest start time for each session to determine session execution order
		if _, exists := sessionStartTimes[sid]; !exists || queryStart.Before(sessionStartTimes[sid]) {
			sessionStartTimes[sid] = queryStart
		}

		// Track read/write access for each table used in the query to build session-level table access map
		// This helps identify potential data conflicts and replay dependencies between sessions
		for _, table := range extractUsedTables(query.Request) {
			table = strings.ToLower(table)
			if sessionAccessMap[sid][table] == nil {
				sessionAccessMap[sid][table] = &tableAccess{}
			}
			if isWriteQuery(query.Request) {
				sessionAccessMap[sid][table].write = true
			} else {
				sessionAccessMap[sid][table].read = true
			}
		}
	}
	return sessionGroups, sessionAccessMap, sessionStartTimes
}

// topologicalSort returns a valid execution order of sessions based on their dependency graph.
// It ensures that all dependencies are honored—i.e., a session appears after all the sessions it depends on.
// If a cycle is detected in the dependency graph, it returns an error.
func topologicalSort(
	sessions map[string][]workloadQuery, // All sessions and their associated queries
	dependencyGraph map[string]map[string]bool, // Map of sessionID -> set of sessions it depends on
) ([]string, error) {
	// Initialize in-degree (number of incoming dependencies) for each session
	inDegree := make(map[string]int)
	for sessionID := range sessions {
		inDegree[sessionID] = 0
	}
	for _, dependentSessions := range dependencyGraph {
		for depSessionID := range dependentSessions {
			inDegree[depSessionID]++
		}
	}

	// Collect all sessions with no dependencies (in-degree = 0)
	executableQueue := make([]string, 0)
	for sessionID, degree := range inDegree {
		if degree == 0 {
			executableQueue = append(executableQueue, sessionID)
		}
	}

	// Perform topological sorting
	executionOrder := make([]string, 0, len(sessions))
	for len(executableQueue) > 0 {
		currentSession := executableQueue[0]
		executableQueue = executableQueue[1:]
		executionOrder = append(executionOrder, currentSession)

		// For each session dependent on currentSession, reduce its in-degree
		for dependent := range dependencyGraph[currentSession] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				executableQueue = append(executableQueue, dependent)
			}
		}
	}

	// If not all sessions were sorted, a cycle exists in the dependency graph
	if len(executionOrder) != len(sessions) {
		return nil, fmt.Errorf("cycle detected in session dependencies")
	}

	return executionOrder, nil
}

func isSystemTable(tableName string) bool {
	return strings.HasPrefix(tableName, "v_monitor.") ||
		strings.HasPrefix(tableName, "v_internal.") ||
		strings.HasPrefix(tableName, "v_catalog.")
}

// buildSessionDependencies constructs a dependency graph between sessions.
// A session is said to depend on another if both access the same table and
// at least one of them performs a write operation. This ensures that write
// conflicts are respected during workload replay.
func (op *nmaWorkloadReplayOp) buildSessionDependencies(
	sessionAccessMap map[string]map[string]*tableAccess, // Maps sessionID -> table name -> access type (read/write)
	sessionStartTimes map[string]time.Time, // Maps sessionID -> earliest query start time
) map[string]map[string]bool { // Returns: sessionID -> set of dependent sessionIDs
	dependencyGraph := make(map[string]map[string]bool)

	for sessionAID, accessMapA := range sessionAccessMap {
		for sessionBID, accessMapB := range sessionAccessMap {
			if sessionAID == sessionBID {
				continue // Skip comparing the session with itself
			}

			for tableName, accessA := range accessMapA {
				if isSystemTable(tableName) {
					continue // skip dependency checks for system tables
				}
				if accessB, exists := accessMapB[tableName]; exists {
					if accessA.write || accessB.write {
						var earlier, later string
						if sessionStartTimes[sessionAID].Before(sessionStartTimes[sessionBID]) {
							earlier, later = sessionAID, sessionBID
						} else {
							earlier, later = sessionBID, sessionAID
						}

						if _, ok := dependencyGraph[later]; !ok {
							dependencyGraph[later] = make(map[string]bool)
						}
						dependencyGraph[later][earlier] = true

						op.logger.Log.Info("Dependency recorded",
							"dependentSession", later,
							"dependsOnSession", earlier,
							"table", tableName,
							"accessA", fmt.Sprintf("read=%v write=%v", accessA.read, accessA.write),
							"accessB", fmt.Sprintf("read=%v write=%v", accessB.read, accessB.write),
						)
					}
				}
			}
		}
	}
	return dependencyGraph
}

func getGID() string {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := bytes.Fields(buf[:n])[1]
	return string(idField)
}

// runReplayGraph executes sessions concurrently while ensuring inter-session dependencies are honored.
// Each session is replayed in its own goroutine only after all sessions it depends on have completed.
//
// Parameters:
//   - execContext: execution context containing cancellation and state control
//   - sessionGroups: map of sessionID -> list of queries for that session
//   - sessionDeps: dependency graph (sessionID -> set of sessionIDs it depends on)
//   - sortedSessions: list of sessionIDs in topological order
//   - sessionStartTimes:  Map of sessionID -> timestamp of the first query in that session;
//     used to calculate accurate timing delays per session.
//   - replayStartTime: time at which replay execution began
func (op *nmaWorkloadReplayOp) runReplayGraph(
	execContext *opEngineExecContext,
	sessionGroups map[string][]workloadQuery,
	sessionDeps map[string]map[string]bool,
	sortedSessions []string,
) {
	var wg sync.WaitGroup

	// Tracks completion status of each session
	runningSignals := make(map[string]chan struct{})
	for sessionID := range sessionGroups {
		runningSignals[sessionID] = make(chan struct{})
	}

	// Concurrency tracking
	var mu sync.Mutex
	currentRunning := 0
	maxConcurrent := 0

	for _, sessionID := range sortedSessions {
		dependencies := sessionDeps[sessionID]

		wg.Add(1)
		go func(sessionID string, dependencies map[string]bool) {
			defer wg.Done()
			op.logger.Log.Info("Goroutine started for session", "sessionID", sessionID, "timestamp",
				time.Now().Format(time.RFC3339Nano))

			// Wait for dependencies to complete
			for depID := range dependencies {
				<-runningSignals[depID]
			}

			// Log session start and increment concurrency count
			mu.Lock()
			currentRunning++
			if currentRunning > maxConcurrent {
				maxConcurrent = currentRunning
			}
			op.logger.Log.Info("Session started",
				"sessionID", sessionID,
				"currentConcurrentSessions", currentRunning)
			op.logger.Log.Info("Starting replay for session", "sessionID", sessionID,
				"thread", getGID(), "startTime", time.Now().Format(time.RFC3339Nano))

			mu.Unlock()

			// Run the session
			op.replaySessionQueries(
				execContext,
				sessionID,
				sessionGroups[sessionID],
			)

			// Log session end and decrement concurrency count
			mu.Lock()
			currentRunning--
			op.logger.Log.Info("Completed replay for session", "sessionID", sessionID,
				"thread", getGID(), "endTime", time.Now().Format(time.RFC3339Nano))
			op.logger.Log.Info("Session finished",
				"sessionID", sessionID,
				"currentConcurrentSessions", currentRunning)
			mu.Unlock()

			// Mark session complete
			close(runningSignals[sessionID])
		}(sessionID, dependencies)
	}

	wg.Wait()
	op.logger.Log.Info("All sessions completed", "maxConcurrentSessions", maxConcurrent)
}

// replaySessionQueries replays all queries from a session in order
func (op *nmaWorkloadReplayOp) replaySessionQueries(
	execContext *opEngineExecContext,
	sessionID string,
	queries []workloadQuery,
) {
	op.logger.Log.Info("Started replaying session", "sessionID", sessionID, "queryCount", len(queries))

	for index := range queries {
		query := &queries[index]

		if execContext.workloadReplyCtx.Err() != nil {
			op.logger.Log.Info("Session canceled", "sessionID", sessionID)
			return
		}

		op.logger.Log.Info("Replaying query",
			"sessionID", sessionID,
			"queryIndex", index,
			"query", query.Request,
			"thread", getGID(),
			"time", time.Now().Format(time.RFC3339Nano),
		)

		if err := op.handleQueryExecutionLifecycle(execContext, query, sessionID, index); err != nil {
			op.appendReplayErrorRow(err)
		}
	}

	op.logger.Log.Info("Finished replaying session", "sessionID", sessionID)
}

// handleQueryExecutionLifecycle orchestrates the end-to-end execution of a single query.
// It preprocesses the SQL, prepares the request, executes it, and processes the result.
// Any failure in the pipeline will be wrapped and returned as an error.
//
// Parameters:
// - execContext: execution context containing state, cancellation, and metadata
// - query: pointer to the workloadQuery to execute
// - sessionID: identifier of the session this query belongs to
// - index: position of the query in the session sequence
func (op *nmaWorkloadReplayOp) handleQueryExecutionLifecycle(
	execContext *opEngineExecContext,
	query *workloadQuery,
	sessionID string,
	queryIndex int,
) error {
	// Step 1: Preprocess the SQL query (e.g., for path rewriting, parameter substitution)
	preprocessor := &vWorkloadPreprocessCall{
		bodyParams: VWorkloadPreprocessParams{
			VRequest:     query.Request, // Original SQL query
			VCatalogPath: "/",           // Default catalog path (could be dynamic)
		},
	}
	if err := preprocessor.PreprocessQuery(); err != nil {
		return fmt.Errorf("query preprocessing failed: %w", err)
	}

	// Get the potentially modified SQL after preprocessing
	rewrittenSQL := preprocessor.getResponse()

	// Step 2: Prepare the query for execution (e.g., request headers, bindings)
	if err := op.prepareRequest(query, rewrittenSQL); err != nil {
		return fmt.Errorf("query preparation failed: %w", err)
	}

	// Step 3: Execute the SQL query
	if err := op.runExecute(execContext); err != nil {
		return fmt.Errorf("query execution failed: %w", err)
	}

	// Step 4: Process results or response from execution
	if err := op.processResult(execContext); err != nil {
		return fmt.Errorf("result processing failed: %w", err)
	}

	// Log successful execution
	op.logger.Log.Info(
		"Query executed successfully",
		"sessionID", sessionID,
		"index", queryIndex,
		"sql", query.Request,
	)

	return nil
}

func (op *nmaWorkloadReplayOp) execute(execContext *opEngineExecContext) error {
	startTime := time.Now() // Record start time
	err := op.executeWorkloadReplay(execContext)
	duration := time.Since(startTime) // Calculate elapsed time
	op.logger.Log.Info("Workload replay execution completed",
		"name", op.name,
		"duration", duration.String())

	return err
}

func (op *nmaWorkloadReplayOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaWorkloadReplayOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong certificate for NMA service on host %s",
				op.name, host)
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		responseObj := workloadQuery{}
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// Save the replay data
		op.workloadReplayData.replayData = append(op.workloadReplayData.replayData, responseObj)
		return nil
	}

	return allErrs
}
