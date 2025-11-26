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
	"math"
	"strconv"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type ReturnEpochQuery struct {
	Epoch int64 `json:"get_last_good_epoch"`
}

type EpochInfo struct {
	Epoch          string `json:"get_last_good_epoch"`
	Timestamp      string `json:"timestamp"`
	CatalogVersion string `json:"catalog_version"`
	KSafety        string `json:"k_safety"`
	Hostname       string `json:"hostname"`
	AHMEpoch       string `json:"ahm_epoch"`
	AHMTimestamp   string `json:"ahm_timestamp"`
}

type LastGoodEpoch struct {
	lastGoodEpoch int64
	lastTimestamp string
	counter       int
}

func NewLastGoodEpoch(epoch int64, timestamp string) *LastGoodEpoch {
	return &LastGoodEpoch{
		lastGoodEpoch: epoch,
		lastTimestamp: timestamp,
		counter:       1,
	}
}

func (lge *LastGoodEpoch) Increment() {
	lge.counter++
}

type VReturnEpochOptions struct {
	DatabaseOptions
}

func VReturnEpochFactory() VReturnEpochOptions {
	options := VReturnEpochOptions{}
	// set default values to the params
	options.setDefaultValues()

	return options
}

func (options *VReturnEpochOptions) validateRequiredOptions(_ vlog.Printer) error {
	return nil
}

func (options *VReturnEpochOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VReturnEpochOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}
	return nil
}

func (options *VReturnEpochOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VReturnEpoch retrieves the latest epoch from the database
func (vcc VClusterCommands) VReturnEpoch(options *VReturnEpochOptions) (int64, error) {
	returnEpoch := []EpochInfo{}
	var nodeInfoInstructions []clusterOp

	err := options.validateUserName(vcc.Log)
	if err != nil {
		return 0, err
	}

	err = options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return 0, err
	}

	vdb := makeVCoordinationDatabase()

	// try to get epoch from running database first
	err = vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)

	if err == nil {
		returnEpochQuery := []ReturnEpochQuery{}
		httpInstructions, err := vcc.produceGetEpochInstructions(options, &returnEpochQuery)
		if err != nil {
			return 0, fmt.Errorf("failed to produce epoch instructions, %w", err)
		}

		httpClusterOpEngine := makeClusterOpEngine(httpInstructions, options)
		httpRunError := httpClusterOpEngine.run(vcc.Log)
		if httpRunError != nil {
			vcc.Log.Error(httpRunError, "HTTPS call failed: %v")
			return 0, fmt.Errorf("failed to get epoch from up database: %w", httpRunError)
		}

		if len(returnEpochQuery) > 0 {
			vcc.Log.Info("Successfully retrieved epoch via HTTPS: %d", returnEpochQuery[0].Epoch)
			return returnEpochQuery[0].Epoch, nil
		}
	}

	// fall back to direct catalog access
	vcc.DisplayInfo("Unable to get last good epoch. Database may be down. Attempting direct catalog access.")

	// retrieve node info for down db
	nmaGetNodesInfoOp := makeNMAGetNodesInfoOp(options.Hosts, options.DBName, options.CatalogPrefix, false, &vdb)
	nodeInfoInstructions = append(nodeInfoInstructions, &nmaGetNodesInfoOp)
	clusterOpEngine := makeClusterOpEngine(nodeInfoInstructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return 0, fmt.Errorf("failed to retrieve node info: %w", runError)
	}

	// produce epoch info instructions
	instructions, err := vcc.produceEpochInfoInstructions(&vdb, options, &returnEpoch)
	if err != nil {
		return 0, fmt.Errorf("failed to produce epoch info instructions, %w", err)
	}

	// give the instructions to the VClusterOpEngine to run
	clusterOpEngine = makeClusterOpEngine(instructions, options)
	runError = clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return 0, fmt.Errorf("failed to get epoch info: %w", runError)
	}

	// Calculate Last Good Epoch
	epoch, err := vcc.calculateLastGoodEpoch(returnEpoch, vcc.Log)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate last good epoch: %w", err)
	}

	return epoch, nil
}

func (vcc VClusterCommands) produceEpochInfoInstructions(vdb *VCoordinationDatabase,
	options *VReturnEpochOptions,
	returnEpoch *[]EpochInfo) ([]clusterOp, error) {
	var instructions []clusterOp

	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}

	nmaHealthOp := makeNMAHealthOp(options.Hosts)

	nmaReturnEpochData := nmaEpochInfoRequestData{}
	hostCatPathMap, err := buildHostCatalogPathMap(options.Hosts, vdb)
	if err != nil {
		return nil, err
	}

	nmaReturnEpochOp := makeNMAEpochInfoOp(options.Hosts, &nmaReturnEpochData,
		returnEpoch, hostCatPathMap)

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaReturnEpochOp)
	return instructions, nil
}

func (vcc VClusterCommands) produceGetEpochInstructions(options *VReturnEpochOptions,
	returnEpoch *[]ReturnEpochQuery) ([]clusterOp, error) {
	var instructions []clusterOp

	hosts := options.Hosts
	initiator := getInitiator(hosts)
	initiatorHost := []string{initiator}

	err := options.setUsePasswordAndValidateUsernameIfNeeded(vcc.Log)
	if err != nil {
		return instructions, err
	}

	nmaHealthOp := makeNMAHealthOp(initiatorHost)

	nmaReturnEpochOp, err := makeNMAGetEpochOp(initiatorHost, options.DBName,
		options.UserName, options.Password, returnEpoch)
	if err != nil {
		return nil, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaReturnEpochOp)
	return instructions, nil
}

// Returns parsed values and whether they are valid for processing
func validateEpochInfo(info *EpochInfo, logger vlog.Printer) (epoch, ksafety int64, valid bool) {
	// Parse epoch value
	parsedEpoch, err := strconv.ParseInt(info.Epoch, 10, 64)
	if err != nil {
		logger.Log.Info("Skipping host %s: invalid epoch value %s", info.Hostname, info.Epoch)
		return 0, 0, false
	}

	// Parse ksafety value
	parsedKsafety, err := strconv.ParseInt(info.KSafety, 10, 64)
	if err != nil {
		logger.Log.Info("Skipping host %s: invalid ksafety value %s", info.Hostname, info.KSafety)
		return 0, 0, false
	}

	// Check for invalid epochs
	if parsedEpoch == -1 || parsedEpoch == math.MinInt64 || parsedKsafety < 0 {
		logger.Log.Info("Skipping host %s: invalid epoch (%d) or ksafety (%d)", info.Hostname, parsedEpoch, parsedKsafety)
		return 0, 0, false
	}

	return parsedEpoch, parsedKsafety, true
}

// Returns the calculated last good epoch
func (vcc VClusterCommands) calculateLastGoodEpoch(epochInfoList []EpochInfo, logger vlog.Printer) (int64, error) {
	if len(epochInfoList) == 0 {
		return 0, fmt.Errorf("no epoch info provided")
	}

	epochMap := make(map[int64]*LastGoodEpoch)
	clusterSafety := int64(-1) // reported ksafety, must be consistent
	totalNodes := len(epochInfoList)

	logger.Log.Info("Calculating last good epoch for %d nodes", totalNodes)

	// Process each host's epoch info
	for _, info := range epochInfoList {
		logger.Log.Info("Considering host %s entry: epoch=%s, timestamp=%s, ksafety=%s",
			info.Hostname, info.Epoch, info.Timestamp, info.KSafety)

		// Validate and parse epoch info
		epoch, ksafety, valid := validateEpochInfo(&info, logger)
		if !valid {
			if _, exists := epochMap[0]; !exists {
				epochMap[0] = NewLastGoodEpoch(0, "0")
			} else {
				epochMap[0].Increment()
			}
			continue
		}

		// Add or increment this epoch in our map
		if lge, exists := epochMap[epoch]; exists {
			lge.Increment()
		} else {
			epochMap[epoch] = NewLastGoodEpoch(epoch, info.Timestamp)
		}

		// Check for consistent ksafety values
		if clusterSafety == -1 {
			clusterSafety = ksafety
		}

		// Can't have inconsistent ksafety values reported by the nodes
		if clusterSafety != ksafety {
			return 0, fmt.Errorf("inconsistent ksafety: host %s reported ksafety %d, cluster ksafety was %d",
				info.Hostname, ksafety, clusterSafety)
		}
	}

	logger.Log.Info("Done looking at epoch info on all hosts, reporting results:")
	for epoch, entry := range epochMap {
		logger.Log.Info("Epoch %d: count=%d, timestamp=%s", epoch, entry.counter, entry.lastTimestamp)
	}

	// Compute epoch with max count
	logger.Log.Info("Computing cluster LGE")
	var bestLGE *LastGoodEpoch
	for _, entry := range epochMap {
		if bestLGE == nil || entry.counter > bestLGE.counter {
			bestLGE = entry
		}
	}

	if bestLGE == nil {
		return 0, fmt.Errorf("no valid epochs found")
	}

	logger.Log.Info("LGE for cluster determined to be: epoch=%d, count=%d, timestamp=%s",
		bestLGE.lastGoodEpoch, bestLGE.counter, bestLGE.lastTimestamp)

	// Check if we have majority consensus
	if bestLGE.counter <= totalNodes/2 {
		return 0, fmt.Errorf("failed to find majority of nodes (%d) reporting the same ASR epoch (largest was epoch %d, count %d)",
			totalNodes, bestLGE.lastGoodEpoch, bestLGE.counter)
	}

	logger.Log.Info("Returning Epoch '%d' at '%s'", bestLGE.lastGoodEpoch, bestLGE.lastTimestamp)

	return bestLGE.lastGoodEpoch, nil
}
