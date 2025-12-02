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
	mu                   sync.Mutex      // protects replayData during concurrent writes
}

type nmaWorkloadReplayOp struct {
	opBase
	nmaWorkloadReplayRequestData
	hosts              []string
	hostNodeMap        vHostNodeMap
	hostRequestBodyMap map[string]string

	workloadReplayData *workloadReplayData
	JobID              int64
	quickReplay        bool // if true, executes queries without delay
	parallelReplay     bool
	preprocessMu       sync.Mutex
}

func makeNMAWorkloadReplayOp(hosts []string, usePassword bool, hostNodeMap vHostNodeMap,
	workloadReplayRequestData *nmaWorkloadReplayRequestData,
	workloadReplayData *workloadReplayData) (*nmaWorkloadReplayOp, error) {
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
			return &op, err
		}
		op.UserName = workloadReplayRequestData.UserName
		op.Password = workloadReplayRequestData.Password
	}

	return &op, nil
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
		op.nmaWorkloadReplayRequestData.FileDir = query.VFileDir

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
	parsedTime, err := time.Parse(time.RFC3339Nano, timestamp)
	if err == nil {
		return parsedTime, nil
	}
	// Fallback to legacy format
	const fallbackFormat = "2006-01-02T15:04:05.999999-07:00"
	parsedTime, err = time.Parse(fallbackFormat, timestamp)
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

func (op *nmaWorkloadReplayOp) executeSequentialReplay(execContext *opEngineExecContext, originalStartTime time.Time) error {
	originalData := op.workloadReplayData.originalWorkloadData
	replayStartTime := time.Now()

	for index, workloadQuery := range originalData {
		select {
		case <-execContext.workloadReplyCtx.Done():
			op.logger.Log.Info("%s: Workload replay canceled: %v", op.name, execContext.workloadReplyCtx.Err())
			return execContext.workloadReplyCtx.Err()
		default:
			replayProgress := fmt.Sprintf("%d/%d", index+1, len(originalData))

			if !op.quickReplay {
				// Determine if we're behind or ahead of schedule
				originalQueryStartTime, err := parseWorkloadTime(workloadQuery.StartTimestamp)
				if err != nil {
					return err // Shouldn't happen since we do validation ahead of time
				}
				originalElapsedTime := originalQueryStartTime.Sub(originalStartTime)
				currentElapsedTime := time.Since(replayStartTime)
				// If we're ahead of schedule, sleep
				if currentElapsedTime < originalElapsedTime {
					sleepDuration := originalElapsedTime - currentElapsedTime
					op.logger.Log.Info("Workload replay ahead of schedule, sleeping", "name",
						op.name, "progress", replayProgress, "duration", sleepDuration)
					select {
					case <-time.After(sleepDuration):
						// Sleep finished, continue to the next iteration
					case <-execContext.workloadReplyCtx.Done():
						op.logger.Log.Info("%s: Workload replay canceled during sleep: %v", op.name, execContext.workloadReplyCtx.Err())
						return execContext.workloadReplyCtx.Err()
					}
				} else {
					op.logger.Log.Info("Workload replay behind schedule, running next query", "name", op.name, "progress", replayProgress)
				}
			} else {
				// Quick replay mode — run queries immediately
				op.logger.Log.Info("Quick replay mode active — running next query immediately", "name", op.name, "progress", replayProgress)
			}

			// Preprocess query
			w := &vWorkloadPreprocessCall{
				bodyParams: VWorkloadPreprocessParams{
					VRequest:     workloadQuery.Request,
					VCatalogPath: "/"}}
			err := w.PreprocessQuery(op.logger.Log)
			if err != nil {
				op.appendReplayErrorRow(err)
				continue
			}
			preprocessedQuery := w.getResponse()

			// Send request to NMA to run the query
			op.logger.Log.Info("Replaying query "+workloadQuery.Request, "name", op.name, "progress", replayProgress)
			err = op.prepareRequest(&workloadQuery, preprocessedQuery)
			if err != nil {
				op.appendReplayErrorRow(err)
				continue
			}
			err = op.runExecute(execContext)
			if err != nil {
				select {
				case <-execContext.workloadReplyCtx.Done():
					op.logger.Log.Info("%s: runExecute canceled: %v", op.name, execContext.workloadReplyCtx.Err())
					return execContext.workloadReplyCtx.Err()
				default:
					op.appendReplayErrorRow(err)
					continue
				}
			}
			// Process/store results of replayed query
			err = op.processResult(execContext)
			if err != nil {
				op.appendReplayErrorRow(err)
			}
		}
	}

	op.logger.Log.Info("Finished sequential workload replay", "name", op.name)
	return nil
}

func (op *nmaWorkloadReplayOp) executeParallelReplay(execContext *opEngineExecContext) error {
	originalData := op.workloadReplayData.originalWorkloadData

	sessionGroups, sessionAccessMap, sessionStartTimes := op.groupSessions(originalData)
	sessionDependencyGraph := op.buildSessionDependencies(sessionAccessMap, sessionStartTimes)
	sortedSessions, err := topologicalSort(sessionGroups, sessionDependencyGraph)
	if err != nil {
		return err
	}

	op.logger.Log.Info("Started executing workload replay with dependency awareness", "name", op.name)
	op.runReplayGraph(execContext, sessionGroups, sessionDependencyGraph, sortedSessions)
	op.logger.Log.Info("Finished executing parllel workload replay", "name", op.name)
	return nil
}

var (
	writeRe = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|MERGE|CREATE|DROP|ALTER|TRUNCATE)\b`)

	// Matches table-like identifiers after SQL clauses.
	// Allows optional IF NOT EXISTS after TABLE (so "CREATE TABLE IF NOT EXISTS t" won't capture "IF")
	// Captures identifiers that can include schema (schema.table), underscores and quoted identifiers
	tablePattern = regexp.MustCompile(
		`(?i)\b(?:FROM|JOIN|INTO|UPDATE|TABLE|DELETE\s+FROM)\s+` +
			`(?:IF\s+NOT\s+EXISTS\s+)?("?[a-zA-Z0-9_]+\."?"?[a-zA-Z0-9_]+"?)\b`)
)

// normalizeTableName trims whitespace and quotes from a table name and converts it to lowercase
func normalizeTableName(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"`)
	return strings.ToLower(raw)
}

// extractUsedTables extracts table names from a SQL query for dependency tracking
// It ignores accidental matches with SQL keywords like "if" or "exists".
func extractUsedTables(sqlText string) []string {
	matches := tablePattern.FindAllStringSubmatch(sqlText, -1)
	tableNames := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			table := normalizeTableName(match[1])
			// ignore common SQL keywords and empty matches
			if table == "if" || table == "exists" || table == "" {
				continue
			}
			tableNames = append(tableNames, table)
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
			// if the SQL query is a write operation (INSERT/UPDATE/DELETE/CREATE/etc)
			if writeRe.MatchString(query.Request) {
				sessionAccessMap[sid][table].write = true
			} else {
				sessionAccessMap[sid][table].read = true
			}
		}
	}
	return sessionGroups, sessionAccessMap, sessionStartTimes
}

func isSystemTable(tableName string) bool {
	return strings.HasPrefix(tableName, "v_monitor.") ||
		strings.HasPrefix(tableName, "v_internal.") ||
		strings.HasPrefix(tableName, "v_catalog.")
}

// buildSessionDependencies analyzes all session table accesses and constructs a dependency graph.
// Each session may depend on earlier sessions if they access the same table and at least one performs a write.
// The result ensures correct execution order when replaying workloads in dependency-aware or parallel mode.
func (op *nmaWorkloadReplayOp) buildSessionDependencies(
	sessionAccessMap map[string]map[string]*tableAccess, // sessionID -> tableName -> access type (read/write)
	sessionStartTimes map[string]time.Time, // sessionID -> earliest query start time
) map[string]map[string]bool { // Returns dependencyGraph: sessionID -> set of dependent sessionIDs
	// Each session is added to the graph even if it has no dependencies.
	dependencyGraph := make(map[string]map[string]bool, len(sessionAccessMap))
	for sessionID := range sessionAccessMap {
		dependencyGraph[sessionID] = make(map[string]bool)
	}

	// Compare every session pair (A,B) to check if B depends on A based on
	// table overlap and read/write access rules.
	for sessionA := range sessionAccessMap {
		for sessionB := range sessionAccessMap {
			if sessionA == sessionB {
				continue // skip self-comparison
			}
			op.evaluateDependencyPair(sessionA, sessionB, sessionAccessMap, sessionStartTimes, dependencyGraph)
		}
	}

	var dependencySummary []string
	for laterSession, dependencySet := range dependencyGraph {
		if len(dependencySet) == 0 {
			dependencySummary = append(dependencySummary, fmt.Sprintf("%s: (no dependencies)", laterSession))
			continue
		}
		var earlierSessions []string
		for dep := range dependencySet {
			earlierSessions = append(earlierSessions, dep)
		}
		dependencySummary = append(
			dependencySummary,
			fmt.Sprintf("%s -> [%s]", laterSession, strings.Join(earlierSessions, ", ")),
		)
	}
	op.logger.Log.Info("Constructed session dependency graph",
		"dependencyOverview", strings.Join(dependencySummary, " | "))

	return dependencyGraph
}

// evaluateDependencyPair checks for data dependencies between two sessions.
// It compares table accesses to determine if one session depends on another
func (op *nmaWorkloadReplayOp) evaluateDependencyPair(
	sessionAID, sessionBID string,
	sessionAccessMap map[string]map[string]*tableAccess,
	sessionStartTimes map[string]time.Time,
	dependencyGraph map[string]map[string]bool,
) {
	// Determine which session started earlier and which started later
	earlierSession, laterSession, valid := determineOrder(sessionAID, sessionBID, sessionStartTimes)
	if !valid {
		return
	}
	earlierAccesses := sessionAccessMap[earlierSession]
	laterAccesses := sessionAccessMap[laterSession]
	for tableName, earlierAccess := range earlierAccesses {
		if isSystemTable(tableName) {
			continue
		}
		laterAccess, exists := laterAccesses[tableName]
		// both sessions access the same table
		if exists && shouldDepend(earlierAccess, laterAccess) {
			addDependencyEdge(op, dependencyGraph, earlierSession, laterSession, tableName, earlierAccess, laterAccess)
			continue
		}
	}
}

// addDependencyEdge adds a directed edge to the dependency graph.
func addDependencyEdge(
	op *nmaWorkloadReplayOp,
	dependencyGraph map[string]map[string]bool,
	earlierSession, laterSession, tableName string,
	accessA, accessB *tableAccess,
) {
	// Initialize map for the later session if needed
	if dependencyGraph[laterSession] == nil {
		dependencyGraph[laterSession] = make(map[string]bool)
	}
	// Avoid duplicate edges
	if dependencyGraph[laterSession][earlierSession] {
		return
	}
	// Record the dependency
	dependencyGraph[laterSession][earlierSession] = true

	op.logger.Log.Info("Dependency recorded",
		"dependentSession", laterSession,
		"dependsOnSession", earlierSession,
		"table", tableName,
		"accessEarlier", fmt.Sprintf("read=%v write=%v", accessA.read, accessA.write),
		"accessLater", fmt.Sprintf("read=%v write=%v", accessB.read, accessB.write))
}

// determineOrder returns the earlier and later session IDs based on start time.
// ok = false if both sessions have the same start time.
func determineOrder(
	sessionID1, sessionID2 string,
	sessionStartTimes map[string]time.Time,
) (earlierSessionID, laterSessionID string, ok bool) {
	startTime1, exists1 := sessionStartTimes[sessionID1]
	startTime2, exists2 := sessionStartTimes[sessionID2]

	// If either time is missing, we can't order reliably - return false.
	if !exists1 || !exists2 {
		return "", "", false
	}

	// If times are different, use them.
	if startTime1.Before(startTime2) {
		return sessionID1, sessionID2, true
	}
	if startTime2.Before(startTime1) {
		return sessionID2, sessionID1, true
	}

	// Same timestamp: deterministically order by sessionID to avoid skipping dependency.
	if sessionID1 < sessionID2 {
		return sessionID1, sessionID2, true
	}
	return sessionID2, sessionID1, true
}

// shouldDepend determines if later depends on earlier
func shouldDepend(earlierAccess, laterAccess *tableAccess) bool {
	// If earlier writes and later reads or writes -> dependency exists
	if earlierAccess.write && (laterAccess.read || laterAccess.write) {
		return true
	}
	// If earlier reads and later writes -> dependency exists
	if earlierAccess.read && laterAccess.write {
		return true
	}
	// No dependency
	return false
}

// topologicalSort determines a valid session execution order from the given dependency graph.
// Each session will appear only after all sessions it depends on have completed.
func topologicalSort(
	sessions map[string][]workloadQuery, // SessionID -> list of queries
	dependencyGraph map[string]map[string]bool, // SessionID -> dependent sessions (later -> earlier)
) ([]string, error) {
	// Compute how many dependencies each session has
	inDegreeMap := initializeInDegree(sessions, dependencyGraph)
	// Build the reverse mapping (earlier -> later sessions)
	successorMap := buildSuccessors(dependencyGraph)
	// Identify sessions that can start immediately (no dependencies)
	readyQueue := collectZeroInDegreeSessions(inDegreeMap, sessions)
	// Perform topological sort to find valid execution order
	sortedOrder, err := topologicalSortSessions(inDegreeMap, successorMap, readyQueue, sessions)
	if err != nil {
		return nil, err
	}
	// ensure all sessions were included
	if len(sortedOrder) != len(sessions) {
		return nil, fmt.Errorf(
			"cycle detected or missing sessions: sorted=%d total=%d",
			len(sortedOrder), len(sessions),
		)
	}

	return sortedOrder, nil
}

// initializeInDegree calculates the in-degree (number of incoming dependencies)
// for each session. Sessions with in-degree = 0 can start immediately.
func initializeInDegree(
	sessions map[string][]workloadQuery,
	dependencyGraph map[string]map[string]bool,
) map[string]int {
	inDegree := make(map[string]int, len(sessions))

	// Initialize all sessions with zero dependencies
	for sessionID := range sessions {
		inDegree[sessionID] = 0
	}

	// Populate in-degree counts based on dependencies
	for sessionID, predecessors := range dependencyGraph {
		inDegree[sessionID] = len(predecessors)

		// Ensure all predecessor sessions exist in the in-degree map
		for predecessor := range predecessors {
			if _, exists := inDegree[predecessor]; !exists {
				inDegree[predecessor] = 0
			}
		}
	}
	return inDegree
}

// buildSuccessors reverses the dependency graph mapping from (later -> earlier)
// to (earlier -> later). This helps identify which sessions can be triggered
// after a particular session completes.
func buildSuccessors(
	dependencyGraph map[string]map[string]bool,
) map[string]map[string]bool {
	successors := make(map[string]map[string]bool)

	// Reverse the edges: later -> earlier becomes earlier -> later
	for laterSession, predecessors := range dependencyGraph {
		for earlierSession := range predecessors {
			if successors[earlierSession] == nil {
				successors[earlierSession] = make(map[string]bool)
			}
			successors[earlierSession][laterSession] = true
		}

		// Ensure each session appears as a key, even if it has no successors
		if successors[laterSession] == nil {
			successors[laterSession] = make(map[string]bool)
		}
	}
	return successors
}

// collectZeroInDegreeSessions identifies sessions that have no dependencies
// and can be started immediately.
func collectZeroInDegreeSessions(
	inDegree map[string]int,
	sessionWorkloads map[string][]workloadQuery,
) []string {
	readySessions := make([]string, 0, len(inDegree))
	for sessionID, degree := range inDegree {
		if degree == 0 {
			if _, exists := sessionWorkloads[sessionID]; exists {
				readySessions = append(readySessions, sessionID)
			}
		}
	}
	return readySessions
}

// topologicalSortSessions computes a valid session execution sequence
// that respects all dependency relationships between sessions.
func topologicalSortSessions(
	inDegreeMap map[string]int, // SessionID -> number of remaining dependencies
	successorMap map[string]map[string]bool, // SessionID -> set of dependent sessionIDs
	readyQueue []string, // Sessions ready to start
	sessionWorkloads map[string][]workloadQuery, // SessionID -> workload queries
) ([]string, error) {
	sortedSessions := make([]string, 0, len(sessionWorkloads))
	processed := make(map[string]bool, len(sessionWorkloads))

	for len(readyQueue) > 0 {
		// Pop the first ready session
		currentSession := readyQueue[0]
		readyQueue = readyQueue[1:]

		// Skip if already processed (duplicate guard)
		if processed[currentSession] {
			continue
		}
		processed[currentSession] = true
		sortedSessions = append(sortedSessions, currentSession)

		// Reduce dependency count for sessions depending on currentSession
		for dependentSession := range successorMap[currentSession] {
			inDegreeMap[dependentSession]--
			if inDegreeMap[dependentSession] == 0 {
				if _, exists := sessionWorkloads[dependentSession]; exists && !processed[dependentSession] {
					readyQueue = append(readyQueue, dependentSession)
				}
			}
		}
	}
	// If no sessions were sorted, a cycle or data issue likely exists
	if len(sortedSessions) == 0 {
		return nil, fmt.Errorf("no sessions could be sorted (possible cycle)")
	}
	return sortedSessions, nil
}

func getGID() string {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := bytes.Fields(buf[:n])[1]
	return string(idField)
}

// runReplayGraph executes sessions concurrently while ensuring inter-session dependencies are honored.
// Each session is replayed in its own goroutine only after all sessions it depends on have completed.
func (op *nmaWorkloadReplayOp) runReplayGraph(
	execContext *opEngineExecContext,
	sessionGroups map[string][]workloadQuery, // sessionID -> list of queries
	sessionDeps map[string]map[string]bool, // sessionID -> dependencies
	sortedSessions []string, // topological order of sessions
) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Signal channels used to notify dependents when a session completes
	doneSignals := make(map[string]chan struct{})
	for sessionID := range sessionGroups {
		doneSignals[sessionID] = make(chan struct{})
	}

	currentRunning := 0
	maxConcurrent := 0
	errCh := make(chan error, len(sessionGroups))

	op.logger.Log.Info("Starting dependency-aware replay", "sessionCount", len(sessionGroups),
		"timestamp", time.Now().Format(time.RFC3339Nano))

	// Launch each session in a separate goroutine, waiting on dependencies via channels
	for _, sessionID := range sortedSessions {
		dependencies := sessionDeps[sessionID]

		wg.Add(1)
		go func(sID string, deps map[string]bool) {
			defer wg.Done()

			// Wait for all dependencies to complete
			for dep := range deps {
				op.logger.Log.Info("Waiting for dependency", "sessionID", sID, "dependsOn", dep)
				select {
				case <-doneSignals[dep]: // dependency completed
				case <-execContext.workloadReplyCtx.Done(): // cancellation
					op.logger.Log.Info("Replay canceled while waiting on dependency",
						"sessionID", sID, "dependsOn", dep)
					errCh <- execContext.workloadReplyCtx.Err()
					return
				}
			}

			// Start session execution
			mu.Lock()
			currentRunning++
			if currentRunning > maxConcurrent {
				maxConcurrent = currentRunning
			}
			op.logger.Log.Info("Session started",
				"sessionID", sID,
				"currentConcurrentSessions", currentRunning)
			mu.Unlock()

			// Run the queries for this session
			err := op.replaySessionQueries(execContext, sID, sessionGroups[sID])
			if err != nil {
				errCh <- fmt.Errorf("session %s: %w", sID, err)
			}

			// Mark session as complete
			mu.Lock()
			currentRunning--
			op.logger.Log.Info("Session finished",
				"sessionID", sID,
				"currentConcurrentSessions", currentRunning)
			mu.Unlock()

			close(doneSignals[sID]) // Signal dependents that this session completed
		}(sessionID, dependencies)
	}

	wg.Wait()

	op.logger.Log.Info("All sessions completed",
		"maxConcurrentSessions", maxConcurrent,
		"timestamp", time.Now().Format(time.RFC3339Nano))

	close(errCh)
}

// replaySessionQueries replays all queries from a session in order
func (op *nmaWorkloadReplayOp) replaySessionQueries(
	execContext *opEngineExecContext,
	sessionID string,
	queries []workloadQuery,
) error {
	op.logger.Log.Info("Started replaying session", "sessionID", sessionID, "queryCount", len(queries))

	var sessionErrs error
	for index := range queries {
		query := &queries[index]

		if execContext.workloadReplyCtx.Err() != nil {
			op.logger.Log.Info("Session canceled", "sessionID", sessionID)
			return execContext.workloadReplyCtx.Err()
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
			sessionErrs = errors.Join(sessionErrs, err)
			// continue to try remaining queries in the session
		}
	}

	op.logger.Log.Info("Finished replaying session", "sessionID", sessionID)
	return sessionErrs
}

// handleQueryExecutionLifecycle orchestrates the end-to-end execution of a single query.
// It preprocesses the SQL, prepares the request, executes it, and processes the result.
// Any failure in the pipeline will be wrapped and returned as an error.
func (op *nmaWorkloadReplayOp) handleQueryExecutionLifecycle(
	execContext *opEngineExecContext,
	query *workloadQuery,
	sessionID string,
	queryIndex int,
) error {
	op.preprocessMu.Lock()
	// Preprocess the SQL query
	preprocessor := &vWorkloadPreprocessCall{
		bodyParams: VWorkloadPreprocessParams{
			VRequest:     query.Request, // Original SQL query
			VCatalogPath: "/",           // Default catalog path (could be dynamic)
		},
	}

	if err := preprocessor.PreprocessQuery(op.logger.Log); err != nil {
		return fmt.Errorf("query preprocessing failed: %w", err)
	}

	// Get the potentially modified SQL after preprocessing
	rewrittenSQL := preprocessor.getResponse()

	if err := op.prepareRequest(query, rewrittenSQL); err != nil {
		return fmt.Errorf("query preparation failed: %w", err)
	}
	op.preprocessMu.Unlock()

	// Execute the SQL query
	if err := op.runExecute(execContext); err != nil {
		return fmt.Errorf("query execution failed: %w", err)
	}
	// Process results or response from execution
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

// Execute workload replay - replay a set of queries
func (op *nmaWorkloadReplayOp) executeWorkloadReplay(execContext *opEngineExecContext) error {
	originalData := op.workloadReplayData.originalWorkloadData
	if len(originalData) == 0 {
		return nil
	}

	originalStartTime, err := parseWorkloadTime(originalData[0].StartTimestamp)
	if err != nil {
		return err
	}

	op.logger.Log.Info("Started executing workload replay", "name", op.name)

	if op.parallelReplay {
		return op.executeParallelReplay(execContext)
	}
	return op.executeSequentialReplay(execContext, originalStartTime)
}

func (op *nmaWorkloadReplayOp) execute(execContext *opEngineExecContext) error {
	return op.executeWorkloadReplay(execContext)
}

func (op *nmaWorkloadReplayOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaWorkloadReplayOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			allErrs = errors.Join(allErrs, fmt.Errorf("[%s] wrong certificate for NMA service on host %s", op.name, host))
			continue
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
		op.workloadReplayData.mu.Lock()
		op.workloadReplayData.replayData = append(op.workloadReplayData.replayData, responseObj)
		op.workloadReplayData.mu.Unlock()
	}
	return allErrs
}
