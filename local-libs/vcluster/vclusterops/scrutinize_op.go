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
)

// top level handler for scrutinize operations
const scrutinizeURLPrefix = "scrutinize/"

// scrutinizeOpBase, in addition to embedding the standard OpBase, wraps some
// common data and functionality for scrutinize-specific ops
type scrutinizeOpBase struct {
	opBase
	id                 string
	batch              string
	urlSuffix          string
	httpMethod         string
	hostNodeNameMap    map[string]string // must correspond to host list exactly!
	hostCatPathMap     map[string]string // must correspond to host list exactly, if non-nil
	hostRequestBodyMap map[string]string // should be nil if not used
}

func (op *scrutinizeOpBase) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		nodeName := op.hostNodeNameMap[host]

		httpRequest := hostHTTPRequest{}
		httpRequest.Method = op.httpMethod
		httpRequest.buildNMAEndpoint(scrutinizeURLPrefix + op.id + "/" + nodeName + "/" + op.batch + op.urlSuffix)
		if op.hostRequestBodyMap != nil {
			httpRequest.RequestData = op.hostRequestBodyMap[host]
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

// processeStagedItemsResult is a parameterized function which contains common logic
// for processing the results of staging various types of items, e.g. vertica.log,
// system tables, etc.
func processStagedItemsResult[T any](op *scrutinizeOpBase, itemList []T) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		if result.isPassing() {
			if result.content == "" {
				// note that an empty response (nothing staged) is not an error
				op.logger.Info("nothing staged on host", "Host", host)
				continue
			}
			// the response is an array of item info structs
			err := op.parseAndCheckResponse(host, result.content, &itemList)
			if err != nil {
				err = fmt.Errorf("[%s] fail to parse result on host %s, details: %w", op.name, host, err)
				allErrs = errors.Join(allErrs, err)
				continue
			}
			for _, entry := range itemList {
				op.logger.Info("item staged on host", "Host", host, "Item", entry)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
