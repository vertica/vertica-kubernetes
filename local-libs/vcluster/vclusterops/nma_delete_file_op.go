package vclusterops

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

const (
	delFileOpName            = "NMADeleteFileOp"
	delFileOpDesc            = "Delete file"
	delVerticaConfFileOpName = "NMADeleteVerticaConfigFileOp"
	delVerticaConfFileOpDesc = "Delete Vertica config file"
)

// this op is for deleting a single file on the specified hosts
type nmaDeleteFileOp struct {
	opBase
	// use this option if deleting the same file on the specified hosts
	filePath           string
	hostRequestBodyMap map[string]string
}

type deleteFileData struct {
	FilePath string `json:"file_path"`
}

func makeNMADeleteFileOp(hosts []string, filePath string) (nmaDeleteFileOp, error) {
	op := nmaDeleteFileOp{}
	op.name = delFileOpName
	op.description = delFileOpDesc
	op.hosts = hosts
	op.filePath = filePath
	err := op.setupRequestBody()
	if err != nil {
		return op, err
	}
	return op, nil
}

func makeNMADeleteVerticaConfFilesOp(hosts []string, vdb *VCoordinationDatabase) (nmaDeleteFileOp, error) {
	op := nmaDeleteFileOp{}
	op.name = delVerticaConfFileOpName
	op.description = delVerticaConfFileOpDesc
	op.hosts = hosts
	err := op.buildDeleteVerticaConfRequestBody(vdb)
	if err != nil {
		return op, err
	}
	return op, nil
}

// make https json data for deleting the same file on specified hosts
func (op *nmaDeleteFileOp) setupRequestBody() error {
	op.hostRequestBodyMap = make(map[string]string, len(op.hosts))

	for _, host := range op.hosts {
		requestData := deleteFileData{}
		requestData.FilePath = op.filePath

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}
		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	op.logger.Info("request data", "op name", op.name, "hostRequestBodyMap", op.hostRequestBodyMap)
	return nil
}

// build https request body by looking at the vdb values
func (op *nmaDeleteFileOp) buildDeleteVerticaConfRequestBody(vdb *VCoordinationDatabase) error {
	op.hostRequestBodyMap = make(map[string]string, len(op.hosts))
	for h, vnode := range vdb.HostNodeMap {
		p := deleteFileData{}
		p.FilePath = vnode.CatalogPath + "/" + util.VerticaConf

		dataBytes, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}
		op.hostRequestBodyMap[h] = string(dataBytes)
	}
	op.logger.Info("request data", "op name", op.name, "hostRequestBodyMap", op.hostRequestBodyMap)
	return nil
}

func (op *nmaDeleteFileOp) setupClusterHTTPRequest(hostRequestBodyMap map[string]string) error {
	for host, requestBody := range hostRequestBodyMap {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("files/delete")
		httpRequest.RequestData = requestBody
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaDeleteFileOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hostRequestBodyMap)
}

func (op *nmaDeleteFileOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaDeleteFileOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaDeleteFileOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			// the response object will be a map e.g,.:
			// {'/tmp/dummy_file.txt':  'deleted'}
			responseObj, err := op.parseAndCheckMapResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}

			_, ok := responseObj["delete_file_return_code"]
			if !ok {
				err = fmt.Errorf(`[%s] response does not contain field "delete_file_return_code"`, op.name)
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
