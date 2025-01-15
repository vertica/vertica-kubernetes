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
	"github.com/theckman/yacspin"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type requestDispatcher struct {
	opBase
	pool adapterPool
}

func makeHTTPRequestDispatcher(logger vlog.Printer) requestDispatcher {
	newHTTPRequestDispatcher := requestDispatcher{}
	newHTTPRequestDispatcher.name = "HTTPRequestDispatcher"
	newHTTPRequestDispatcher.logger = logger.WithName(newHTTPRequestDispatcher.name)

	return newHTTPRequestDispatcher
}

// set up the pool connection for each host
func (dispatcher *requestDispatcher) setup(hosts []string) {
	dispatcher.pool = getPoolInstance(dispatcher.logger)

	dispatcher.pool.connections = make(map[string]adapter)
	for _, host := range hosts {
		adapter := makeHTTPAdapter(dispatcher.logger)
		adapter.host = host
		dispatcher.pool.connections[host] = &adapter
	}
}

// set up the pool connection for each host to download a file
func (dispatcher *requestDispatcher) setupForDownload(hosts []string,
	hostToFilePathsMap map[string]string) {
	dispatcher.pool = getPoolInstance(dispatcher.logger)

	for _, host := range hosts {
		adapter := makeHTTPDownloadAdapter(dispatcher.logger, hostToFilePathsMap[host])
		adapter.host = host
		dispatcher.pool.connections[host] = &adapter
	}
}

func (dispatcher *requestDispatcher) sendRequest(httpRequest *clusterHTTPRequest, spinner *yacspin.Spinner) error {
	dispatcher.logger.Info("HTTP request dispatcher's sendRequest is called")
	return dispatcher.pool.sendRequest(httpRequest, spinner)
}
