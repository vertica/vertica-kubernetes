package vclusterops

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	delDirOpName          = "NMADeleteDirectoriesOp"
	delDirOpDesc          = "Delete database directories"
	delDirRetainDirOpName = "NMADeleteDirsRetainDirOp"
	delDirRetainDirOpDesc = "Delete database directories except for specified dirs: "
	// the real dir storing catalog content
	nodeCatalogSubDirSuffix = "/Catalog"
)

type nmaDeleteDirectoriesOp struct {
	opBase
	hostRequestBodyMap        map[string]string
	sandbox                   bool
	forceDelete               bool
	retainDirsExceptCatSubDir bool // used for drop db, if true, only drop v_<node_name>_catalog/Catalog subdir, retain other dirs
	retainOnlyDepotDir        bool // used for remove subcluster/node, if true, retain the depot dir of the node
}

type deleteDirParams struct {
	Directories []string `json:"directories"`
	ForceDelete bool     `json:"force_delete"`
	Sandbox     bool     `json:"sandbox"`
}

func makeNMADeleteDirOpHelper(vdb *VCoordinationDatabase,
	forceDelete, retainDirsExceptCatSubDir, retainOnlyDepotDir bool) (nmaDeleteDirectoriesOp, error) {
	op := nmaDeleteDirectoriesOp{}
	op.name = delDirOpName
	op.description = delDirOpDesc
	op.retainDirsExceptCatSubDir = retainDirsExceptCatSubDir
	op.retainOnlyDepotDir = retainOnlyDepotDir
	if op.retainDirs() {
		op.name = delDirRetainDirOpName
		retainDirList := []string{}
		if op.retainDirsExceptCatSubDir {
			retainDirList = append(retainDirList, "non /Catalog dirs")
		}
		if op.retainOnlyDepotDir {
			retainDirList = append(retainDirList, "depot dir")
		}
		op.description = delDirRetainDirOpDesc + strings.Join(retainDirList, ",")
	}
	op.hosts = vdb.HostList
	// op.sandbox being false indicates that this is NOT an unsandbox operation
	// when unsandboxing, removing the entire v_<node_name>_catalog dir is mandatory
	// in other cases, one may choose to retain the catalog dirs
	op.sandbox = false

	err := op.buildRequestBody(vdb, forceDelete)
	if err != nil {
		return op, err
	}

	return op, nil
}

func makeNMADeleteDirectoriesOp(
	vdb *VCoordinationDatabase,
	forceDelete bool, retainDirsExceptCatSubDir, retainOnlyDepotDir bool) (nmaDeleteDirectoriesOp, error) {
	op, err := makeNMADeleteDirOpHelper(vdb, forceDelete, retainDirsExceptCatSubDir, retainOnlyDepotDir)
	if err != nil {
		return op, err
	}

	return op, nil
}

func makeNMADeleteDirsSandboxOp(
	hosts []string,
	forceDelete bool,
	sandbox bool,
) (nmaDeleteDirectoriesOp, error) {
	op := nmaDeleteDirectoriesOp{}
	op.name = delDirOpName
	op.description = delDirOpDesc
	op.sandbox = sandbox
	op.forceDelete = forceDelete
	op.hosts = hosts
	return op, nil
}

func (op *nmaDeleteDirectoriesOp) retainDirs() bool {
	return op.retainDirsExceptCatSubDir || op.retainOnlyDepotDir
}

func (op *nmaDeleteDirectoriesOp) buildRequestBody(
	vdb *VCoordinationDatabase,
	forceDelete bool) error {
	op.hostRequestBodyMap = make(map[string]string)
	for h, vnode := range vdb.HostNodeMap {
		p := deleteDirParams{}

		// directories
		dbCatalogPath := filepath.Join(vdb.CatalogPrefix, vdb.Name)
		dbDataPath := filepath.Join(vdb.DataPrefix, vdb.Name)
		dbDepotPath := ""
		if vdb.UseDepot {
			dbDepotPath = filepath.Join(vdb.DepotPrefix, vdb.Name)
		}
		// case 1: remove all directories
		if !op.retainDirs() {
			// most common case: remove everything -- catalog, data, depot and storage locations
			p.Directories = append(p.Directories, vnode.CatalogPath, dbCatalogPath, dbDataPath)
			if vdb.UseDepot {
				p.Directories = append(p.Directories, vnode.DepotPath, dbDepotPath)
			}
			p.Directories = append(p.Directories, vnode.StorageLocations...)
		} else {
			// case 2: retain some directories
			// if retainNonCatalogDir
			// we only remove the v_<nodename>_catalog/Catalog directory
			if op.retainDirsExceptCatSubDir {
				p.Directories = append(p.Directories, vnode.CatalogPath+nodeCatalogSubDirSuffix)
				op.logger.Info("user specified retaining catalog directory of the database")
			} else if op.retainOnlyDepotDir {
				// remove everything except for depot dirs
				p.Directories = append(p.Directories, vnode.CatalogPath)
				if dbDepotPath != dbCatalogPath {
					p.Directories = append(p.Directories, dbCatalogPath)
				}
				if dbDepotPath != dbDataPath {
					p.Directories = append(p.Directories, dbDataPath)
				}
				// actual data path will be removed as a storage location
				p.Directories = append(p.Directories, vnode.StorageLocations...)
			}
		}

		// force-delete
		p.ForceDelete = forceDelete
		p.Sandbox = op.sandbox

		dataBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail: %w", op.name, err)
		}
		op.hostRequestBodyMap[h] = string(dataBytes)

		op.logger.Info("delete directory params", "host", h, "params", p)
	}

	return nil
}

func (op *nmaDeleteDirectoriesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("directories/delete")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}
	return nil
}

func (op *nmaDeleteDirectoriesOp) prepare(execContext *opEngineExecContext) error {
	if op.sandbox {
		if len(execContext.scNodesInfo) == 0 {
			return fmt.Errorf(`[%s] Cannot find any node information of target subcluster in OpEngineExecContext`, op.name)
		}
		hosts := []string{}
		op.hostRequestBodyMap = make(map[string]string)

		for i := range execContext.scNodesInfo {
			node := &execContext.scNodesInfo[i]
			p := deleteDirParams{}
			// op.sandbox being true indicates that this is an unsandbox operation
			// removing the entire v_<node_name>_catalog dir is mandatory
			p.Directories = append(p.Directories, node.CatalogPath)
			p.ForceDelete = true
			p.Sandbox = op.sandbox
			dataBytes, err := json.Marshal(p)
			if err != nil {
				return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail: %w", op.name, err)
			}
			op.hostRequestBodyMap[node.Address] = string(dataBytes)

			op.logger.Info("delete directory params", "host", node.Address, "params", p)

			hosts = append(hosts, node.Address)
		}
		if len(op.hosts) == 0 {
			op.hosts = hosts
		}
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaDeleteDirectoriesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaDeleteDirectoriesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaDeleteDirectoriesOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			// the response object will be a map[string]string, for example:
			// {
			//     "/data/test_db": "deleted",
			//     "/data/test_db/v_demo_db_node0001_catalog": "deleted",
			//     "/data/test_db/v_demo_db_node0001_data": "deleted"
			// }
			_, err := op.parseAndCheckMapResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
