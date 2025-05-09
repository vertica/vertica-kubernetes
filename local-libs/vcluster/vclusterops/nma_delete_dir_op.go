package vclusterops

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

const (
	delDirOpName                = "NMADeleteDirectoriesOp"
	delDirOpDesc                = "Delete database directories"
	delDirRetainCatlogDirOpName = "NMADeleteDirsRetainCatalogDirOp"
	delDirRetainCatlogDirOpDesc = "Delete database directories except for catalog directory"
	// the real dir storing catalog content
	nodeCatalogSubDirSuffix = "/Catalog"
)

type nmaDeleteDirectoriesOp struct {
	opBase
	hostRequestBodyMap map[string]string
	sandbox            bool
	forceDelete        bool
	retainCatalogDir   bool
}

type deleteDirParams struct {
	Directories []string `json:"directories"`
	ForceDelete bool     `json:"force_delete"`
	Sandbox     bool     `json:"sandbox"`
}

func makeNMADeleteDirectoriesOp(
	vdb *VCoordinationDatabase,
	forceDelete bool,
) (nmaDeleteDirectoriesOp, error) {
	op := nmaDeleteDirectoriesOp{}
	op.name = delDirOpName
	op.description = delDirOpDesc
	op.hosts = vdb.HostList
	op.sandbox = false
	op.retainCatalogDir = false
	err := op.buildRequestBody(vdb, forceDelete)
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

func makeNMADeleteDirsRetainCatalogDirOp(
	vdb *VCoordinationDatabase,
	forceDelete bool,
	retainCatalogDir bool) (nmaDeleteDirectoriesOp, error) {
	op := nmaDeleteDirectoriesOp{}
	op.name = delDirRetainCatlogDirOpName
	op.description = delDirRetainCatlogDirOpDesc
	op.hosts = vdb.HostList
	op.sandbox = false
	op.retainCatalogDir = retainCatalogDir
	err := op.buildRequestBody(vdb, forceDelete)
	if err != nil {
		return op, err
	}

	return op, nil
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
		// most common case
		if !op.retainCatalogDir {
			p.Directories = append(p.Directories, vnode.CatalogPath, dbCatalogPath, dbDataPath)
		} else {
			p.Directories = append(p.Directories, vnode.CatalogPath+nodeCatalogSubDirSuffix)
			// avoid removing catalog path in case they have the same prefix as data path
			if dbDataPath != dbCatalogPath {
				p.Directories = append(p.Directories, dbDataPath)
			}
			op.logger.Info("user specified retaining catalog directory of the database")
		}

		p.Directories = append(p.Directories, vnode.StorageLocations...)
		if vdb.UseDepot {
			dbDepotPath := filepath.Join(vdb.DepotPrefix, vdb.Name)
			p.Directories = append(p.Directories, vnode.DepotPath)
			// case 1: no need to retain catalog dir, remove everything
			// case 2: specified to retain catalog dir, yet dbCatalogPath is differ from dbDepotPath, remove dbDepotPath
			if (!op.retainCatalogDir) || (dbDepotPath != dbCatalogPath) {
				p.Directories = append(p.Directories, dbDepotPath)
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
