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
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/theckman/yacspin"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type adapterPool struct {
	logger vlog.Printer
	// map from host to HTTPAdapter
	connections map[string]adapter
}

var (
	poolInstance adapterPool
	once         sync.Once
)

// return a new instance of an adapterPool. The adapterPool cannot be shared
// between Go routines. Otherwise, they will clobber each other state causing
// HTTP request errors. It is the callers responsibility to ensure it doesn't
// get shared.
func getPoolInstance(logger vlog.Printer) adapterPool {
	/* if once.Do(f) is called multiple times,
	 * only the first call will invoke f,
	 * even if f has a different value in each invocation.
	 * Reference: https://pkg.go.dev/sync#Once
	 */
	once.Do(func() {
		poolInstance = makeAdapterPool(logger)
	})

	return poolInstance
}

func makeAdapterPool(logger vlog.Printer) adapterPool {
	newAdapterPool := adapterPool{}
	newAdapterPool.connections = make(map[string]adapter)
	newAdapterPool.logger = logger.WithName("AdapterPool")
	return newAdapterPool
}

type adapterToRequest struct {
	adapter adapter
	request hostHTTPRequest
}

func (pool *adapterPool) sendRequest(httpRequest *clusterHTTPRequest, spinner *yacspin.Spinner) error {
	// build a collection of adapter to request
	// we need this step as a host may not be in the pool
	// in that case, we should not proceed
	var adapterToRequestCollection []adapterToRequest
	for host := range httpRequest.RequestCollection {
		request := httpRequest.RequestCollection[host]
		adpt, ok := pool.connections[host]
		if !ok {
			return fmt.Errorf("host %s is not found in the adapter pool", host)
		}
		ar := adapterToRequest{adapter: adpt, request: request}
		adapterToRequestCollection = append(adapterToRequestCollection, ar)
	}

	hostCount := len(adapterToRequestCollection)

	// result channel to collect result from each host
	resultChannel := make(chan hostHTTPResult, hostCount)

	// only track the progress of HTTP requests for vcluster CLI
	if pool.logger.ForCli {
		// use context to check whether a step has completed
		ctx, cancelCtx := context.WithCancel(context.Background())
		go progressCheck(ctx, httpRequest.Name, pool.logger, spinner)
		// cancel the progress check context when the result channel is closed
		defer cancelCtx()
	}

	for i := 0; i < len(adapterToRequestCollection); i++ {
		ar := adapterToRequestCollection[i]
		// send request to the hosts
		// each goroutine will handle one request for one host
		request := ar.request
		go ar.adapter.sendRequest(&request, resultChannel)
	}

	// handle results
	// we expect to receive the same number of results from the channel as the number of hosts
	// before proceeding to the next steps
	httpRequest.ResultCollection = make(map[string]hostHTTPResult)
	for i := 0; i < hostCount; i++ {
		result, ok := <-resultChannel
		if ok {
			httpRequest.ResultCollection[result.host] = result
		}
	}
	close(resultChannel)

	return nil
}

// progressCheck checks whether a step (operation) has been completed.
// Elapsed time of the step in seconds will be displayed.
func progressCheck(ctx context.Context, name string, logger vlog.Printer, spinner *yacspin.Spinner) {
	const progressCheckInterval = 5
	startTime := time.Now()

	ticker := time.NewTicker(progressCheckInterval * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// context is canceled
			// - when the requests to each host are completed, or
			// - when the timeout is reached
			return
		case tickTime := <-ticker.C:
			elapsedTime := tickTime.Sub(startTime)
			logger.PrintInfo("[%s] is still running. %.f seconds spent at this step.",
				name, elapsedTime.Seconds())
			if spinner != nil {
				spinner.Message(fmt.Sprintf("%.f seconds spent at this step", elapsedTime.Seconds()))
			}
		}
	}
}
