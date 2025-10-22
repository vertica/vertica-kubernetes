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
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
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

// PreprocessQuery implements the main logic
func (p *vWorkloadPreprocessCall) PreprocessQuery() error {
	err := p.validateParams()
	if err != nil {
		return err
	}

	// commented out GenerateCopyQuery as COPY statement is not supported
	// if strings.Contains(strings.ToLower(p.bodyParams.VRequest), "copy") {
	// 	p.GenerateCopyQuery(p.bodyParams.VRequest)
	// } else {
	// 	p.DefaultQuery()
	// }

	p.DefaultQuery()

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

// GenerateCopyQuery if statement is of type COPY
// TODO: This is adding a forward slash at the end of catalog path - should be removed
func (p *vWorkloadPreprocessCall) GenerateCopyQuery(query string) {
	// prefix query
	rePre := regexp.MustCompile(`(?i)copy.*from`) // Panics if the pattern is invalid
	preMatches := rePre.FindAllStringSubmatch(query, -1)

	// postfix query
	rePost := regexp.MustCompile(`(?i)delimiter.*`) // Panics if the pattern is invalid
	postMatches := rePost.FindAllStringSubmatch(query, -1)

	stdinQuery := fmt.Sprintf("%s stdin %s", strings.Join(preMatches[0], " "), strings.Join(postMatches[0], " "))
	fmt.Println("VWorkloadPreprocess Copy stdin SQL: " + stdinQuery)

	reFile := regexp.MustCompile(`'(.*?)'`) // Panics if the pattern is invalid
	absFilePath := reFile.FindStringSubmatch(query)[1]
	fmt.Println("Preprocess path: " + absFilePath)
	fileDir, fileName := filepath.Split(absFilePath)
	if fileDir == "" {
		fileDir = p.bodyParams.VCatalogPath // default catalog path
	}

	p.response.VStmtType = "copy"
	p.response.VParsedQuery = stdinQuery
	p.response.VFileName = fileName
	p.response.VFileDir = fileDir
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
