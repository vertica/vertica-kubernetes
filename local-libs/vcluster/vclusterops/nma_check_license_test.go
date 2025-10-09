package vclusterops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestNMACheckLicenseOp(t *testing.T) {
	vl := vlog.Printer{}
	execContext := makeOpEngineExecContext(vl)
	const host = "host"
	hosts := []string{host}
	username := "check-license"
	dbName := "check-license-db"
	password := "check-license-op"
	licenseFile := "license-data"
	useDBPassword := true

	checkLicenseOp, err := makeNMACheckLicenseOp(hosts, username, dbName, licenseFile, &password, useDBPassword, true, vl)
	assert.NoError(t, err)
	checkLicenseOp.setupBasicInfo()
	err = checkLicenseOp.prepare(&execContext)
	assert.NoError(t, err)
	checkLicenseData := &checkLicenseData{}
	checkLicenseData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, &password)
	checkLicenseData.LicenseFile = licenseFile
	dataBytes, _ := json.Marshal(checkLicenseData)
	assert.Equal(t, string(dataBytes), checkLicenseOp.clusterHTTPRequest.RequestCollection[host].RequestData)
	checkLicensehostHTTPResponse := makeMockOpResponse(host)
	resultMap := map[string]string{}
	resultMap["company_name"] = "Vertica Community Edition "
	resultMapBytes, _ := json.Marshal(resultMap)
	checkLicensehostHTTPResponse.setSuccess()
	checkLicensehostHTTPResponse.hostHTTPResult.content = string(resultMapBytes)
	checkLicenseOp.clusterHTTPRequest.ResultCollection = make(map[string]hostHTTPResult)
	checkLicenseOp.clusterHTTPRequest.ResultCollection[host] = checkLicensehostHTTPResponse.hostHTTPResult
	err = checkLicenseOp.processResult(&execContext)
	assert.Error(t, err)
}
