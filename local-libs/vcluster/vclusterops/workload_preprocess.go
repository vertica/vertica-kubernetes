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
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
)

// A VWorkloadPreprocessParams model.
//
// This is to run preprocess step
type VWorkloadPreprocessParams struct {
	VRequest     string
	VCatalogPath string
}

// A VWorkloadPreprocessResponse model
//
// returns information
type VWorkloadPreprocessResponse struct {
	VStmtType    string
	VParsedQuery string
	VFileName    string
	VFileDir     string
}

// vWorkloadPreprocessCall entry point
type vWorkloadPreprocessCall struct {
	bodyParams VWorkloadPreprocessParams
	response   VWorkloadPreprocessResponse
}

var (
	reCopyFromFileOrStdin = regexp.MustCompile(`(?is)^\s*COPY\s+.*\s+FROM\s+(STDIN\b|'[^']+')`)
	reCopyFromKafkaSource = regexp.MustCompile(`(?is)^\s*COPY\s+.*\s+SOURCE\s+\w+\s*\(`)
)

// PreprocessQuery implements the main logic
func (p *vWorkloadPreprocessCall) PreprocessQuery(logger logr.Logger) error {
	err := p.validateParams()
	if err != nil {
		return err
	}

	query := strings.TrimSpace(p.bodyParams.VRequest)
	logger.Info("Preprocessing query", "query", query)

	switch {
	case reCopyFromFileOrStdin.MatchString(query):
		// Traditional COPY FROM 'file.csv'
		logger.Info("Detected COPY FROM file/stdin", "query", query)
		p.GenerateCopyQueryFromFile(query, logger)

	case reCopyFromKafkaSource.MatchString(query):
		// COPY SOURCE KafkaSource(...)
		logger.Info("Detected COPY SOURCE (Kafka)", "query", query)
		p.GenerateCopyQueryFromKafkaSource(query, logger)

	default:
		// Not a COPY statement at all
		logger.Info("Detected non-COPY statement", "query", query)
		p.DefaultQuery()
	}

	return nil
}

// validateParams validate parmas
func (p *vWorkloadPreprocessCall) validateParams() error {
	// Check if requested fields are empty
	if p.bodyParams.VRequest == "" {
		return fmt.Errorf("field VRequest is empty")
	}

	if p.bodyParams.VCatalogPath == "" {
		return fmt.Errorf("field VCatalogPath is empty")
	}
	return nil
}

// GenerateCopyQueryFromFile if statement is of type COPY
func (p *vWorkloadPreprocessCall) GenerateCopyQueryFromFile(query string, logger logr.Logger) {
	logger.Info("Preprocessing COPY query", "query", query)
	// Extract the full COPY ... FROM ... clause (case-insensitive)
	rePre := regexp.MustCompile(`(?i)copy.*from`)
	preMatches := rePre.FindString(query)
	if preMatches == "" {
		fmt.Println("ERROR: Failed to match COPY prefix in query")
		return
	}

	// Extract the DELIMITER clause (case-insensitive)
	rePost := regexp.MustCompile(`(?i)delimiter.*`)
	postMatches := rePost.FindString(query)
	if postMatches == "" {
		logger.Info("WARNING: DELIMITER clause not found; continuing without it")
		fmt.Println("WARNING: DELIMITER clause not found; continuing without it")
	}

	// Construct COPY ... FROM STDIN statement
	stdinQuery := fmt.Sprintf("%s stdin %s", preMatches, postMatches)
	logger.Info("VWorkloadPreprocess Copy stdin SQL:", stdinQuery)

	// Extract file path inside single quotes
	reFile := regexp.MustCompile(`'(.*?)'`)
	fileMatch := reFile.FindStringSubmatch(query)
	const expectedFileMatchParts = 2 // 2: full match + captured group

	if len(fileMatch) < expectedFileMatchParts {
		logger.Error(errors.New("ERROR: Could not extract file path from COPY statement Query"), "query", query)
		fmt.Println("ERROR: Could not extract file path from COPY statement")
		fmt.Printf("Query: %s\n", query)
		return
	}
	absFilePath := fileMatch[1]
	logger.Info("Preprocess path:", absFilePath)

	// Split file path into directory and file name
	fileDir, fileName := filepath.Split(absFilePath)

	if fileDir == "" {
		fileDir = filepath.Clean(p.bodyParams.VCatalogPath)
		fmt.Println("No directory in file path; falling back to VCatalogPath:", fileDir)
		logger.Info("No directory in file path; falling back to VCatalogPath:", fileDir)
	}

	// Populate response object
	p.response.VStmtType = "copy"
	p.response.VParsedQuery = stdinQuery
	p.response.VFileName = fileName
	p.response.VFileDir = fileDir
}

func (p *vWorkloadPreprocessCall) GenerateCopyQueryFromKafkaSource(query string, logger logr.Logger) {
	// Confirm this is a COPY SOURCE query
	reCopySource := regexp.MustCompile(`(?i)^\s*COPY\s+.*\s+SOURCE\s+\w+\s*\(`)
	if !reCopySource.MatchString(query) {
		err := fmt.Errorf("invalid COPY SOURCE statement")
		logger.Error(err, "Query did not match expected COPY SOURCE pattern", "query", query)
		return
	}

	// Extract stream='...' — required
	reStream := regexp.MustCompile(`(?i)stream\s*=\s*'(.*?)'`)
	streamMatch := reStream.FindStringSubmatch(query)
	if len(streamMatch) < 2 || streamMatch[1] == "" {
		err := fmt.Errorf("missing required 'stream' parameter in COPY SOURCE")
		logger.Error(err, "stream=... not found", "query", query)
		return
	}
	streamValue := streamMatch[1]
	logger.Info("Extracted Kafka stream", "stream", streamValue)

	// Extract brokers='...' — optional
	reBrokers := regexp.MustCompile(`(?i)brokers\s*=\s*'(.*?)'`)
	brokerMatch := reBrokers.FindStringSubmatch(query)
	brokerValue := ""
	const expectedBrokerMatchParts = 2

	if len(brokerMatch) >= expectedBrokerMatchParts {
		brokerValue = brokerMatch[1]
		logger.Info("Extracted Kafka brokers", "brokers", brokerValue)
	} else {
		logger.Info("No brokers=... found in COPY SOURCE", "query", query)
	}

	// Populate response
	p.response.VStmtType = "kafka_copy"
	p.response.VParsedQuery = query
	p.response.VFileName = streamValue // store stream
	p.response.VFileDir = brokerValue  // store brokers

	logger.Info("Kafka COPY SOURCE response set",
		"VStmtType", p.response.VStmtType,
		"VParsedQuery", p.response.VParsedQuery,
		"VFileName (stream)", p.response.VFileName,
		"VFileDir (brokers)", p.response.VFileDir)
}

// DefaultQuery if statement is not of type COPY
func (p *vWorkloadPreprocessCall) DefaultQuery() {
	p.response.VStmtType = ""
	p.response.VParsedQuery = p.bodyParams.VRequest
	p.response.VFileName = ""
	p.response.VFileDir = ""
}

// getResponse return response
func (p *vWorkloadPreprocessCall) getResponse() VWorkloadPreprocessResponse {
	return p.response
}
