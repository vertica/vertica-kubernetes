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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
	"golang.org/x/exp/maps"
)

// VCoordinationDatabase represents catalog and node information for a database. The
// VCreateDatabase command returns a VCoordinationDatabase struct. Operations on
// an existing database (e.g. VStartDatabase) consume a VCoordinationDatabase struct.
type VCoordinationDatabase struct {
	Name string
	// processed path prefixes
	CatalogPrefix string
	DataPrefix    string
	HostNodeMap   vHostNodeMap
	UnboundNodes  []*VCoordinationNode
	// for convenience
	HostList []string // expected to be resolved IP addresses

	// Eon params, the boolean values are for convenience
	IsEon                   bool
	CommunalStorageLocation string
	UseDepot                bool
	DepotPrefix             string
	DepotSize               string
	AwsIDKey                string
	AwsSecretKey            string
	NumShards               int

	// authentication
	LicensePathOnNode string

	// more to add when useful
	Ipv6 bool

	PrimaryUpNodes        []string
	ComputeNodes          []string
	FirstStartAfterRevive bool

	AllSandboxes []string // slice of all sandboxes in the cluster
}

type vHostNodeMap map[string]*VCoordinationNode

func makeVHostNodeMap() vHostNodeMap {
	return make(vHostNodeMap)
}

func makeVCoordinationDatabase() VCoordinationDatabase {
	return VCoordinationDatabase{}
}

func (vdb *VCoordinationDatabase) setFromBasicDBOptions(options *VCreateDatabaseOptions) error {
	// we trust the information in the config file
	// so we do not perform validation here
	vdb.Name = options.DBName
	vdb.CatalogPrefix = options.CatalogPrefix
	vdb.DataPrefix = options.DataPrefix
	vdb.DepotPrefix = options.DepotPrefix

	vdb.IsEon = false
	if options.CommunalStorageLocation != "" {
		vdb.IsEon = true
		vdb.CommunalStorageLocation = options.CommunalStorageLocation
		vdb.DepotPrefix = options.DepotPrefix
		vdb.DepotSize = options.DepotSize
	}

	vdb.UseDepot = false
	if options.DepotPrefix != "" {
		vdb.UseDepot = true
	}

	vdb.HostNodeMap = makeVHostNodeMap()
	for _, address := range options.Hosts {
		vnode := VCoordinationNode{}
		err := vnode.setFromBasicDBOptions(options, address)
		if err != nil {
			return err
		}
		err = vdb.addNode(&vnode)
		if err != nil {
			return err
		}
	}

	return nil
}

// update vdb object with sandbox info of given sandbox name
func (vdb *VCoordinationDatabase) updateSandboxNodeInfo(sandVdb *VCoordinationDatabase, sandboxName string) {
	for _, vnode := range sandVdb.HostNodeMap {
		if vnode.Sandbox == sandboxName {
			vdb.HostNodeMap[vnode.Address] = vnode
			vdb.HostList = append(vdb.HostList, vnode.Address)
		}
	}
}

// populate vdb with main cluster info
func (vdb *VCoordinationDatabase) setMainCluster(mainVdb *VCoordinationDatabase) {
	allSandboxes := mapset.NewSet[string]()
	vdb.IsEon = mainVdb.IsEon
	vdb.UseDepot = mainVdb.UseDepot
	vdb.Name = mainVdb.Name
	vdb.CommunalStorageLocation = mainVdb.CommunalStorageLocation
	vdb.UnboundNodes = mainVdb.UnboundNodes
	vdb.PrimaryUpNodes = mainVdb.PrimaryUpNodes
	vdb.ComputeNodes = mainVdb.ComputeNodes
	vdb.CatalogPrefix = mainVdb.CatalogPrefix
	vdb.HostNodeMap = makeVHostNodeMap()
	for _, vnode := range mainVdb.HostNodeMap {
		if vnode.Sandbox == util.MainClusterSandbox {
			vdb.HostNodeMap[vnode.Address] = vnode
			vdb.HostList = append(vdb.HostList, vnode.Address)
		} else if !allSandboxes.Contains(vnode.Sandbox) {
			allSandboxes.Add(vnode.Sandbox)
		}
	}
	vdb.AllSandboxes = allSandboxes.ToSlice()
}

func (vdb *VCoordinationDatabase) setFromCreateDBOptions(options *VCreateDatabaseOptions, logger vlog.Printer) error {
	// build after validating the options
	err := options.validateAnalyzeOptions(logger)
	if err != nil {
		return err
	}

	err = vdb.setFromBasicDBOptions(options)
	if err != nil {
		return err
	}

	// set additional db info from the create db options
	vdb.HostList = make([]string, len(options.Hosts))
	vdb.HostList = options.Hosts
	vdb.LicensePathOnNode = options.LicensePathOnNode
	vdb.Ipv6 = options.IPv6

	if options.GetAwsCredentialsFromEnv {
		err := vdb.getAwsCredentialsFromEnv()
		if err != nil {
			return err
		}
	}
	vdb.NumShards = options.ShardCount

	return nil
}

// addNode adds a given host to the VDB's HostList and HostNodeMap.
// Duplicate host will not be added.
func (vdb *VCoordinationDatabase) addNode(vnode *VCoordinationNode) error {
	// unbound nodes have the same 0.0.0.0 address,
	// so we add them into the UnboundedNodes list
	if vnode.Address == util.UnboundedIPv4 || vnode.Address == util.UnboundedIPv6 {
		vdb.UnboundNodes = append(vdb.UnboundNodes, vnode)
		return nil
	}

	if _, exist := vdb.HostNodeMap[vnode.Address]; exist {
		return fmt.Errorf("host %s has already been in the VDB's HostList", vnode.Address)
	}

	vdb.HostNodeMap[vnode.Address] = vnode
	vdb.HostList = append(vdb.HostList, vnode.Address)

	return nil
}

// addHosts adds a given list of hosts to the VDB's HostList
// and HostNodeMap. existingHostNodeMap contains entries for nodes
// in all clusters (main and sandboxes)
func (vdb *VCoordinationDatabase) addHosts(hosts []string, scName string,
	existingHostNodeMap vHostNodeMap) error {
	totalHostCount := len(hosts) + len(existingHostNodeMap) + len(vdb.UnboundNodes)
	nodeNameToHost := genNodeNameToHostMap(existingHostNodeMap)
	// The GenVNodeName(...) function below will generate node names based on nodeNameToHost and totalHostCount.
	// If a name already exists, it won't be re-generated.
	// In this case, we need to add unbound node names into this map too.
	// Otherwise, the new nodes will reuse the existing unbound node names, then make a clash later on.
	for _, vnode := range vdb.UnboundNodes {
		nodeNameToHost[vnode.Name] = vnode.Address
	}

	for _, host := range hosts {
		vNode := makeVCoordinationNode()
		name, ok := util.GenVNodeName(nodeNameToHost, vdb.Name, totalHostCount)
		if !ok {
			return fmt.Errorf("could not generate a vnode name for %s", host)
		}
		nodeNameToHost[name] = host
		vNode.setNode(vdb, host, name, scName)
		err := vdb.addNode(&vNode)
		if err != nil {
			return err
		}
	}

	return nil
}

// copy copies the receiver's fields into a new VCoordinationDatabase struct and
// returns that struct. You can choose to copy only a subset of the receiver's hosts
// by passing a slice of hosts to keep.
func (vdb *VCoordinationDatabase) copy(targetHosts []string) VCoordinationDatabase {
	v := VCoordinationDatabase{
		Name:                    vdb.Name,
		CatalogPrefix:           vdb.CatalogPrefix,
		DataPrefix:              vdb.DataPrefix,
		IsEon:                   vdb.IsEon,
		CommunalStorageLocation: vdb.CommunalStorageLocation,
		UseDepot:                vdb.UseDepot,
		DepotPrefix:             vdb.DepotPrefix,
		DepotSize:               vdb.DepotSize,
		AwsIDKey:                vdb.AwsIDKey,
		AwsSecretKey:            vdb.AwsSecretKey,
		NumShards:               vdb.NumShards,
		LicensePathOnNode:       vdb.LicensePathOnNode,
		Ipv6:                    vdb.Ipv6,
		PrimaryUpNodes:          util.CopySlice(vdb.PrimaryUpNodes),
		ComputeNodes:            util.CopySlice(vdb.ComputeNodes),
		AllSandboxes:            util.CopySlice(vdb.AllSandboxes),
	}

	if len(targetHosts) == 0 {
		v.HostNodeMap = util.CopyMap(vdb.HostNodeMap)
		v.HostList = util.CopySlice(vdb.HostList)
		return v
	}

	v.HostNodeMap = util.FilterMapByKey(vdb.HostNodeMap, targetHosts)
	v.HostList = targetHosts

	return v
}

// copyHostNodeMap copies the receiver's HostNodeMap. You can choose to copy
// only a subset of the receiver's hosts by passing a slice of hosts to keep.
func (vdb *VCoordinationDatabase) copyHostNodeMap(targetHosts []string) vHostNodeMap {
	if len(targetHosts) == 0 {
		return util.CopyMap(vdb.HostNodeMap)
	}

	return util.FilterMapByKey(vdb.HostNodeMap, targetHosts)
}

// genNodeNameToHostMap generates a map, with node name as key and
// host ip as value, from HostNodeMap which includes entries for nodes in all clusters.
func genNodeNameToHostMap(existingHostNodeMap vHostNodeMap) map[string]string {
	vnodes := make(map[string]string)
	for h, vnode := range existingHostNodeMap {
		vnodes[vnode.Name] = h
	}
	return vnodes
}

// getSCNames returns a slice of subcluster names which the nodes
// in the current VCoordinationDatabase instance belong to.
func (vdb *VCoordinationDatabase) getSCNames() []string {
	allKeys := make(map[string]bool)
	scNames := []string{}
	for _, vnode := range vdb.HostNodeMap {
		sc := vnode.Subcluster
		if _, value := allKeys[sc]; !value {
			allKeys[sc] = true
			scNames = append(scNames, sc)
		}
	}
	return scNames
}

// containNodes determines which nodes are in the vdb and which ones are not.
// The node is determined by looking up the host address.
func (vdb *VCoordinationDatabase) containNodes(nodes []string) (nodesInDB, nodesNotInDB []string) {
	hostSet := mapset.NewSet(nodes...)
	nodesInDB = []string{}
	for _, vnode := range vdb.HostNodeMap {
		address := vnode.Address
		if exist := hostSet.Contains(address); exist {
			nodesInDB = append(nodesInDB, address)
		}
	}

	if len(nodesInDB) == len(nodes) {
		return nodesInDB, nil
	}
	return nodesInDB, util.SliceDiff(nodes, nodesInDB)
}

// hasAtLeastOneDownNode returns true if the current VCoordinationDatabase instance
// has at least one down node.
func (vdb *VCoordinationDatabase) hasAtLeastOneDownNode() bool {
	for _, vnode := range vdb.HostNodeMap {
		if vnode.State == util.NodeDownState {
			return true
		}
	}

	return false
}

// GenDataPath builds and returns the data path
func (vdb *VCoordinationDatabase) GenDataPath(nodeName string) string {
	dataSuffix := fmt.Sprintf("%s_data", nodeName)
	return filepath.Join(vdb.DataPrefix, vdb.Name, dataSuffix)
}

// GenDepotPath builds and returns the depot path
func (vdb *VCoordinationDatabase) GenDepotPath(nodeName string) string {
	depotSuffix := fmt.Sprintf("%s_depot", nodeName)
	return filepath.Join(vdb.DepotPrefix, vdb.Name, depotSuffix)
}

// GenCatalogPath builds and returns the catalog path
func (vdb *VCoordinationDatabase) GenCatalogPath(nodeName string) string {
	catalogSuffix := fmt.Sprintf("%s_catalog", nodeName)
	return filepath.Join(vdb.CatalogPrefix, vdb.Name, catalogSuffix)
}

// set aws id key and aws secret key
func (vdb *VCoordinationDatabase) getAwsCredentialsFromEnv() error {
	awsIDKey := os.Getenv("AWS_ACCESS_KEY_ID")
	if awsIDKey == "" {
		return fmt.Errorf("unable to get AWS ID key from environment variable")
	}
	awsSecretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if awsSecretKey == "" {
		return fmt.Errorf("unable to get AWS Secret key from environment variable")
	}
	vdb.AwsIDKey = awsIDKey
	vdb.AwsSecretKey = awsSecretKey
	return nil
}

// filterPrimaryNodes will remove secondary nodes from vdb
func (vdb *VCoordinationDatabase) filterPrimaryNodes() {
	primaryHostNodeMap := makeVHostNodeMap()

	for h, vnode := range vdb.HostNodeMap {
		if vnode.IsPrimary {
			primaryHostNodeMap[h] = vnode
		}
	}
	vdb.HostNodeMap = primaryHostNodeMap

	vdb.HostList = maps.Keys(vdb.HostNodeMap)
}

// Update and limit the hostlist based on status and sandbox info
// If sandbox provided, pick up sandbox up hosts and return. Else return up hosts.
func (vdb *VCoordinationDatabase) filterUpHostlist(inputHosts []string, sandbox string) []string {
	var clusterHosts []string
	var upSandboxHosts []string

	for _, h := range inputHosts {
		vnode, ok := vdb.HostNodeMap[h]
		if !ok {
			// host address not found in vdb, skip it
			continue
		}
		if vnode.Sandbox == util.MainClusterSandbox && vnode.State == util.NodeUpState {
			clusterHosts = append(clusterHosts, vnode.Address)
		} else if vnode.Sandbox == sandbox && vnode.State == util.NodeUpState {
			upSandboxHosts = append(upSandboxHosts, vnode.Address)
		}
	}
	if sandbox == util.MainClusterSandbox {
		return clusterHosts
	}
	return upSandboxHosts
}

// hostIsUp returns true if the host is up
func (vdb *VCoordinationDatabase) hostIsUp(hostName string) bool {
	return vdb.HostNodeMap[hostName].State == util.NodeUpState
}

// VCoordinationNode represents node information from the database catalog.
type VCoordinationNode struct {
	Name    string `json:"name"`
	Address string
	// complete paths, not just prefix
	CatalogPath          string `json:"catalog_path"`
	StorageLocations     []string
	UserStorageLocations []string
	DepotPath            string
	// DB client port, should be 5433 by default
	Port int
	// default should be ipv4
	ControlAddressFamily string
	IsPrimary            bool
	State                string
	// empty string if it is not an eon db
	Subcluster string
	// empty string if it is not in a sandbox
	Sandbox       string
	Version       string
	IsControlNode bool
	ControlNode   string
}

func CloneVCoordinationNode(node *VCoordinationNode) *VCoordinationNode {
	if node == nil {
		return nil
	}
	return &VCoordinationNode{
		Name:                 node.Name,
		Address:              node.Address,
		CatalogPath:          node.CatalogPath,
		StorageLocations:     append([]string{}, node.StorageLocations...),     // Create a new slice
		UserStorageLocations: append([]string{}, node.UserStorageLocations...), // Create a new slice
		DepotPath:            node.DepotPath,
		Port:                 node.Port,
		ControlAddressFamily: node.ControlAddressFamily,
		IsPrimary:            node.IsPrimary,
		State:                node.State,
		Subcluster:           node.Subcluster,
		Sandbox:              node.Sandbox,
		Version:              node.Version,
		IsControlNode:        node.IsControlNode,
		ControlNode:          node.ControlNode,
	}
}

func makeVCoordinationNode() VCoordinationNode {
	return VCoordinationNode{}
}

func (vnode *VCoordinationNode) setFromBasicDBOptions(
	options *VCreateDatabaseOptions,
	host string,
) error {
	dbName := options.DBName
	dbNameInNode := strings.ToLower(dbName)
	// compute node name and complete paths for each node
	for i, h := range options.Hosts {
		if h != host {
			continue
		}

		vnode.Address = host
		vnode.Port = options.ClientPort
		nodeNameSuffix := i + 1
		vnode.Name = fmt.Sprintf("v_%s_node%04d", dbNameInNode, nodeNameSuffix)
		catalogSuffix := fmt.Sprintf("%s_catalog", vnode.Name)
		vnode.CatalogPath = filepath.Join(options.CatalogPrefix, dbName, catalogSuffix)
		dataSuffix := fmt.Sprintf("%s_data", vnode.Name)
		dataPath := filepath.Join(options.DataPrefix, dbName, dataSuffix)
		vnode.StorageLocations = append(vnode.StorageLocations, dataPath)
		if options.DepotPrefix != "" {
			depotSuffix := fmt.Sprintf("%s_depot", vnode.Name)
			vnode.DepotPath = filepath.Join(options.DepotPrefix, dbName, depotSuffix)
		}
		if options.IPv6 {
			vnode.ControlAddressFamily = util.IPv6ControlAddressFamily
		} else {
			vnode.ControlAddressFamily = util.DefaultControlAddressFamily
		}

		return nil
	}
	return fmt.Errorf("fail to set up vnode from options: host %s does not exist in options", host)
}

func (vnode *VCoordinationNode) setNode(vdb *VCoordinationDatabase, address, name, scName string) {
	// we trust the information in the config file
	// so we do not perform validation here
	vnode.Address = address
	vnode.Name = name
	vnode.Subcluster = scName
	vnode.CatalogPath = vdb.GenCatalogPath(vnode.Name)
	dataPath := vdb.GenDataPath(vnode.Name)
	vnode.StorageLocations = append(vnode.StorageLocations, dataPath)
	if vdb.DepotPrefix != "" {
		vnode.DepotPath = vdb.GenDepotPath(vnode.Name)
	}
	if vdb.Ipv6 {
		vnode.ControlAddressFamily = util.IPv6ControlAddressFamily
	} else {
		vnode.ControlAddressFamily = util.DefaultControlAddressFamily
	}
}
