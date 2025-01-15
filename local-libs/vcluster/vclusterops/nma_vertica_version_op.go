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
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
)

const (
	NoVersion = "NO_VERSION"
	DefaultSC = "default_subcluster"
)

type hostVersionMap map[string]string

type nmaVerticaVersionOp struct {
	opBase
	IsEon              bool
	RequireSameVersion bool
	HasIncomingSCNames bool
	SCToHostVersionMap map[string]hostVersionMap
	vdb                *VCoordinationDatabase
	sandbox            bool
	scName             string
	readOnly           bool
	targetNodeIPs      []string // used to filter desired nodes' info
	unreachableHosts   []string // hosts that are not reachable through NMA
	skipUnreachable    bool     // whether we skip unreachable host in the subcluster
}

func makeHostVersionMap() hostVersionMap {
	return make(hostVersionMap)
}

func makeSCToHostVersionMap() map[string]hostVersionMap {
	return make(map[string]hostVersionMap)
}

// makeNMACheckVerticaVersionOp is used when db has not been created
func makeNMACheckVerticaVersionOp(hosts []string, sameVersion, isEon bool) nmaVerticaVersionOp {
	op := nmaVerticaVersionOp{}
	op.name = "NMACheckVerticaVersionOp"
	op.description = "Check Vertica version"
	op.hosts = hosts
	op.RequireSameVersion = sameVersion
	op.IsEon = isEon
	op.SCToHostVersionMap = makeSCToHostVersionMap()
	return op
}

// makeNMAReadVerticaVersionOp is used to read Vertica version from each node
// to a VDB object
func makeNMAReadVerticaVersionOp(vdb *VCoordinationDatabase) nmaVerticaVersionOp {
	op := nmaVerticaVersionOp{}
	op.name = "NMAReadVerticaVersionOp"
	op.description = "Read Vertica version"
	op.hosts = vdb.HostList
	op.readOnly = true
	op.vdb = vdb
	op.SCToHostVersionMap = makeSCToHostVersionMap()
	return op
}

// makeNMAVerticaVersionOpWithTargetHosts is used in start_db, VCluster will check Vertica
// version for the subclusters which contain target hosts, ignoring unreachable hosts
func makeNMAVerticaVersionOpWithTargetHosts(sameVersion bool, unreachableHosts, targetNodeIPs []string) nmaVerticaVersionOp {
	// We set hosts to nil and isEon to false temporarily, and they will get the correct value from execute context in prepare()
	op := makeNMACheckVerticaVersionOp(nil /*hosts*/, sameVersion, false /*isEon*/)
	op.targetNodeIPs = targetNodeIPs
	op.unreachableHosts = unreachableHosts
	op.skipUnreachable = true
	return op
}

// makeNMAVerticaVersionOpAfterUnsandbox is used after unsandboxing
func makeNMAVerticaVersionOpAfterUnsandbox(sameVersion bool, scName string) nmaVerticaVersionOp {
	// We set hosts to nil and isEon to true
	op := makeNMACheckVerticaVersionOp(nil /*hosts*/, sameVersion, true /*isEon*/)
	op.sandbox = true
	op.scName = scName
	return op
}

// makeNMAVerticaVersionOpWithVDB is used when db is up
func makeNMAVerticaVersionOpWithVDB(sameVersion bool, vdb *VCoordinationDatabase) nmaVerticaVersionOp {
	// We set hosts to nil temporarily, and it will get the correct value from vdb in prepare()
	op := makeNMACheckVerticaVersionOp(nil /*hosts*/, sameVersion, vdb.IsEon)
	op.vdb = vdb
	return op
}

// makeNMAVerticaVersionOpBeforeStartNode is used in start_node, VCluster will check Vertica
// version for the nodes which are in the same cluster(main cluster or sandbox) as the target hosts
func makeNMAVerticaVersionOpBeforeStartNode(vdb *VCoordinationDatabase, unreachableHosts, targetNodeIPs []string,
	skipUnreachable bool) nmaVerticaVersionOp {
	op := makeNMACheckVerticaVersionOp(nil, true /*sameVersion*/, vdb.IsEon)
	op.unreachableHosts = unreachableHosts
	op.targetNodeIPs = targetNodeIPs
	op.skipUnreachable = skipUnreachable
	op.vdb = vdb
	return op
}

func (op *nmaVerticaVersionOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("vertica/version")
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}
func (op *nmaVerticaVersionOp) prepareSandboxVers(execContext *opEngineExecContext) error {
	// Add current unsandboxed sc hosts
	if len(execContext.scNodesInfo) == 0 {
		return fmt.Errorf(`[%s] Cannot find any node information of target subcluster in OpEngineExecContext`, op.name)
	}
	for _, node := range execContext.scNodesInfo {
		op.hosts = append(op.hosts, node.Address)
	}
	// Add Up main cluster hosts
	if len(execContext.upHostsToSandboxes) == 0 {
		return fmt.Errorf(`[%s] Cannot find any up hosts in OpEngineExecContext`, op.name)
	}
	for h, sb := range execContext.upHostsToSandboxes {
		if sb == "" {
			op.hosts = append(op.hosts, h)
		}
	}
	sc := op.scName
	// initialize the SCToHostVersionMap with empty versions
	op.SCToHostVersionMap[sc] = makeHostVersionMap()
	for _, host := range op.hosts {
		op.SCToHostVersionMap[sc][host] = ""
	}

	return nil
}
func (op *nmaVerticaVersionOp) prepare(execContext *opEngineExecContext) error {
	/*
		 *	 Initialize SCToHostVersionMap in three cases:
		 *	 - when db is up, we initialize SCToHostVersionMap using vdb content (from Vertica https service)
		 *   - when db is down, we initialize SCToHostVersionMap using nmaVDatabase (from NMA /catalog/database) in execute context
		 *   - when db has not been created, we initialize SCToHostVersionMap using op.hosts (from user input)
		 *   An example of initialized SCToHostVersionMap:
		    {
				"default_subcluster" : {"192.168.0.101": "", "192.168.0.102": ""},
				"subcluster1" : {"192.168.0.103": "", "192.168.0.104": ""},
				"subcluster2" : {"192.168.0.105": "", "192.168.0.106": ""},
			}
		 *
	*/
	if op.sandbox {
		err := op.prepareSandboxVers(execContext)
		if err != nil {
			return err
		}
	} else if len(op.hosts) == 0 {
		if op.vdb != nil {
			// db is up
			err := op.buildHostVersionMapWithVDB(execContext)
			if err != nil {
				return err
			}
		} else {
			// start db
			err := op.buildHostVersionMapWhenDBDown(execContext)
			if err != nil {
				return err
			}
		}
	} else {
		op.buildHostVersionMapDefault()
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaVerticaVersionOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaVerticaVersionOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type nmaVerticaVersionOpResponse map[string]string

func (op *nmaVerticaVersionOp) parseAndCheckResponse(host, resultContent string) error {
	// each result is a pair {"vertica_version": <vertica version string>}
	// example result:
	// {"vertica_version": "Vertica Analytic Database v12.0.3"}
	var responseObj nmaVerticaVersionOpResponse
	err := util.GetJSONLogErrors(resultContent, &responseObj, op.name, op.logger)
	if err != nil {
		return err
	}

	version, ok := responseObj["vertica_version"]
	// missing key "vertica_version"
	if !ok {
		return errors.New("Unable to get vertica version from host " + host)
	}

	op.logger.Info("JSON response", "host", host, "responseObj", responseObj)
	// update version for the host in SCToHostVersionMap
	for sc, hostVersionMap := range op.SCToHostVersionMap {
		if _, exists := hostVersionMap[host]; exists {
			op.SCToHostVersionMap[sc][host] = version
		}
	}
	return nil
}

func (op *nmaVerticaVersionOp) logResponseCollectVersions() error {
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isPassing() {
			errStr := fmt.Sprintf("[%s] result from host %s summary %s, details: %+v\n",
				op.name, host, FailureResult, result)
			return errors.New(errStr)
		}

		err := op.parseAndCheckResponse(host, result.content)
		if err != nil {
			return fmt.Errorf("[%s] fail to parse result on host %s, details: %w", op.name, host, err)
		}
	}
	return nil
}

func (op *nmaVerticaVersionOp) logCheckVersionMatch() error {
	/*   An example of SCToHostVersionMap:
	    {
			"default_subcluster" : {"192.168.0.101": "Vertica Analytic Database v24.1.0", "192.168.0.102": "Vertica Analytic Database v24.1.0"},
			"subcluster1" : {"192.168.0.103": "Vertica Analytic Database v24.0.0", "192.168.0.104": "Vertica Analytic Database v24.0.0"},
			"subcluster2" : {"192.168.0.105": "Vertica Analytic Database v24.0.0", "192.168.0.106": "Vertica Analytic Database v24.0.0"},
		}
	*/
	var versionStr string
	for sc, hostVersionMap := range op.SCToHostVersionMap {
		versionStr = NoVersion
		for host, version := range hostVersionMap {
			op.logger.Info("version check", "host", host, "version", version)
			if version == "" {
				if op.IsEon && op.HasIncomingSCNames {
					return fmt.Errorf("[%s] No version collected for host [%s] in subcluster [%s]", op.name, host, sc)
				}
				return fmt.Errorf("[%s] No version collected for host [%s]", op.name, host)
			} else if versionStr == NoVersion {
				// first time seeing a valid version, set it as the versionStr
				versionStr = version
			} else if version != versionStr && op.RequireSameVersion {
				if op.IsEon && op.HasIncomingSCNames {
					return fmt.Errorf("[%s] Found mismatched versions: [%s] and [%s] in subcluster [%s]", op.name, versionStr, version, sc)
				}
				return fmt.Errorf("[%s] Found mismatched versions: [%s] and [%s]", op.name, versionStr, version)
			}
		}
		// no version collected at all
		if versionStr == NoVersion {
			if op.IsEon && op.HasIncomingSCNames {
				return fmt.Errorf("[%s] No version collected for all hosts in subcluster [%s]", op.name, sc)
			}
			return fmt.Errorf("[%s] No version collected for all hosts", op.name)
		}
	}
	return nil
}

func (op *nmaVerticaVersionOp) processResult(_ *opEngineExecContext) error {
	if op.readOnly {
		return op.readVersion()
	}

	err := op.logResponseCollectVersions()
	if err != nil {
		return err
	}

	return op.logCheckVersionMatch()
}

func (op *nmaVerticaVersionOp) readVersion() error {
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		versionMap, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			return fmt.Errorf("[%s] fail to parse result on host %s, details: %w", op.name, host, err)
		}

		// the versionStr looks like
		// Vertica Analytic Database v24.3.0
		versionStr, ok := versionMap["vertica_version"]
		// missing key "vertica_version"
		if !ok {
			return fmt.Errorf("unable to get vertica version from host %s", host)
		}

		vnode, ok := op.vdb.HostNodeMap[host]
		// missing host in vdb
		if !ok {
			return fmt.Errorf("failed to find host %s in the vdb object", host)
		}
		versionInfo := strings.Split(versionStr, " ")
		vnode.Version = versionInfo[len(versionInfo)-1]
	}

	return nil
}

// prepareHostNodeMap is a helper to make a host-node map for nodes in target subclusters
func (op *nmaVerticaVersionOp) prepareHostNodeMap(execContext *opEngineExecContext) (map[string]*nmaVNode, error) {
	hostNodeMap := execContext.nmaVDatabase.HostNodeMap
	if len(op.targetNodeIPs) > 0 {
		hostSCMap := make(map[string]string)
		scHostsMap := make(map[string][]string)
		for host, vnode := range execContext.nmaVDatabase.HostNodeMap {
			hostSCMap[host] = vnode.Subcluster.Name
			scHostsMap[vnode.Subcluster.Name] = append(scHostsMap[vnode.Subcluster.Name], host)
		}
		allHostsInTargetSCs, err := op.findHostsInTargetSubclusters(hostSCMap, scHostsMap)
		if err != nil {
			return hostNodeMap, err
		}
		// get host-node map for all hosts in target subclusters
		hostNodeMap = util.FilterMapByKey(execContext.nmaVDatabase.HostNodeMap, allHostsInTargetSCs)
	}
	return hostNodeMap, nil
}

// prepareHostNodeMapWithVDB is a helper to make a host-node map for all nodes in the
// subclusters of target nodes
func (op *nmaVerticaVersionOp) prepareHostNodeMapWithVDB() (vHostNodeMap, error) {
	if len(op.targetNodeIPs) == 0 {
		return op.vdb.HostNodeMap, nil
	}
	hostNodeMap := makeVHostNodeMap()
	hostSCMap := make(map[string]string)
	scHostsMap := make(map[string][]string)
	for host, vnode := range op.vdb.HostNodeMap {
		hostSCMap[host] = vnode.Subcluster
		scHostsMap[vnode.Subcluster] = append(scHostsMap[vnode.Subcluster], host)
	}
	allHostsInTargetSCs, err := op.findHostsInTargetSubclusters(hostSCMap, scHostsMap)
	if err != nil {
		return hostNodeMap, err
	}
	// get host-node map for all hosts in target subclusters
	hostNodeMap = util.FilterMapByKey(op.vdb.HostNodeMap, allHostsInTargetSCs)

	return hostNodeMap, nil
}

// findHostsInTargetSubclusters is a helper function to get all hosts in the subclusters of
// target nodes. The parameters of this function are two maps:
// 1. host-subcluster map for the entire database
// 2. subcluster-hosts map for the entire database
func (op *nmaVerticaVersionOp) findHostsInTargetSubclusters(hostSCMap map[string]string,
	scHostsMap map[string][]string) ([]string, error) {
	allHostsInTargetSCs := []string{}
	// find subclusters that hold the target hosts
	targetSCs := []string{}
	for _, host := range op.targetNodeIPs {
		sc, ok := hostSCMap[host]
		if ok {
			targetSCs = append(targetSCs, sc)
		} else {
			return allHostsInTargetSCs, fmt.Errorf("[%s] host %s does not exist in the database", op.name, host)
		}
	}
	// find all hosts that in target subclusters
	for _, sc := range targetSCs {
		hosts, ok := scHostsMap[sc]
		if ok {
			// current sc contains unreachablehosts
			if len(util.SliceCommon(hosts, op.unreachableHosts)) > 0 {
				hasUpNodes := false
				if !op.skipUnreachable {
					for _, host := range hosts {
						if op.vdb.hostIsUp(host) {
							hasUpNodes = true
							break
						}
					}
				}
				// remove unreachable hosts
				hosts = util.SliceDiff(hosts, op.unreachableHosts)
				// all nodes in the existing subcluster are unreachable
				if hasUpNodes && len(util.SliceDiff(hosts, op.targetNodeIPs)) == 0 {
					return allHostsInTargetSCs, fmt.Errorf("[%s] all existing nodes in subcluster %s are not reachable", op.name, sc)
				}
			}
			allHostsInTargetSCs = append(allHostsInTargetSCs, hosts...)
		} else {
			return allHostsInTargetSCs, fmt.Errorf("[%s] internal error: subcluster %s was lost when preparing the hosts", op.name, sc)
		}
	}
	return allHostsInTargetSCs, nil
}

func (op *nmaVerticaVersionOp) buildHostVersionMapDefault() {
	// When creating a db, the subclusters of all nodes will be the same so set it to a fixed value.
	sc := DefaultSC
	// initialize the SCToHostVersionMap with empty versions
	op.SCToHostVersionMap[sc] = makeHostVersionMap()
	for _, host := range op.hosts {
		op.SCToHostVersionMap[sc][host] = ""
	}
}

// buildHostVersionMapWhenDBDown sets an hostVersionMap for start_db
func (op *nmaVerticaVersionOp) buildHostVersionMapWhenDBDown(execContext *opEngineExecContext) error {
	op.HasIncomingSCNames = true
	if execContext.nmaVDatabase.CommunalStorageLocation != "" {
		op.IsEon = true
	}
	hostNodeMap, err := op.prepareHostNodeMap(execContext)
	if err != nil {
		return err
	}
	for host, vnode := range hostNodeMap {
		op.hosts = append(op.hosts, host)
		// initialize the SCToHostVersionMap with empty versions
		sc := vnode.Subcluster.Name
		if op.SCToHostVersionMap[sc] == nil {
			op.SCToHostVersionMap[sc] = makeHostVersionMap()
		}
		op.SCToHostVersionMap[sc][host] = ""
	}
	return nil
}

// buildHostVersionMapWithVDB sets an hostVersionMap from a vdb
func (op *nmaVerticaVersionOp) buildHostVersionMapWithVDB(execContext *opEngineExecContext) error {
	op.HasIncomingSCNames = true
	hostNodeMap, err := op.prepareHostNodeMapWithVDB()
	if err != nil {
		return err
	}
	for host, vnode := range hostNodeMap {
		op.hosts = append(op.hosts, host)
		sc := vnode.Subcluster
		// Update subcluster of new nodes that will be assigned to default subcluster.
		// When we created vdb in add_node without specifying subcluster, we did not know the default subcluster name
		// so new nodes is using "" as their subclusters. Below line will correct node nodes' subclusters.
		if op.vdb.IsEon && sc == "" && execContext.defaultSCName != "" {
			op.vdb.HostNodeMap[host].Subcluster = execContext.defaultSCName
			sc = execContext.defaultSCName
		}

		// initialize the SCToHostVersionMap with empty versions
		if op.SCToHostVersionMap[sc] == nil {
			op.SCToHostVersionMap[sc] = makeHostVersionMap()
		}
		op.SCToHostVersionMap[sc][host] = ""
	}
	return nil
}
