package vclusterops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

//nolint:gosec // test uses "hardcoded credentials"
func TestNMARotateTLSCertsOp(t *testing.T) {
	// make a mock op
	host1 := "host1"
	host2 := "host2"
	host3 := "host3"
	hosts := []string{host1, host2, host3}
	hostsToSandboxes := map[string]string{
		host1: "",
		host2: "sb1",
		host3: "sb2",
	}
	username := "test_user"
	pwStr := "test_pw_str"
	dbName := "test_db_name"
	usePW := true
	opData := getMockNMARotateTLSCertsOpData()

	// construction should succeed
	op, err := makeNMARotateTLSCertsOp(hosts, username, dbName, hostsToSandboxes,
		&opData, AWSSecretManagerType, &pwStr, usePW, &tlsConfigInfo{})
	assert.NoError(t, err)

	// run through prepare() phase only
	op.skipExecute = true
	instructions := []clusterOp{&op}
	log := vlog.Printer{}
	var options DatabaseOptions
	clusterOpEngine := makeClusterOpEngine(instructions, &options)
	err = clusterOpEngine.run(log)
	assert.NoError(t, err)

	// check that requests are well-formed
	for _, host := range hosts {
		httpRequest := op.clusterHTTPRequest.RequestCollection[host]
		assert.Equal(t, "v1/vertica/tls/rotate-certs", httpRequest.Endpoint)
		assert.Equal(t, PostMethod, httpRequest.Method)
		data := rotateTLSCertsData{}
		err = json.Unmarshal([]byte(httpRequest.RequestData), &data)

		assert.NoError(t, err)
		assert.Equal(t, username, data.DBUsername)
		assert.Equal(t, pwStr, data.DBPassword)
		assert.Equal(t, dbName, data.DBName)
		assert.Equal(t, opData, data.RotateTLSCertsData)
		assert.Equal(t, awsSecretManagerName, data.SecretManager)
	}
}

func getMockNMARotateTLSCertsOpData() RotateTLSCertsData {
	return RotateTLSCertsData{
		KeySecretName:    "key",
		KeyConfig:        "key_config",
		CertSecretName:   "cert",
		CertConfig:       "cert_config",
		CACertSecretName: "ca_cert",
		CACertConfig:     "ca_cert_config",
		TLSMode:          "try_verify",
		TLSConfig:        "HTTPS",
	}
}

type mockNMARotateTLSCertsOpHTTPResult struct {
	mockOpHTTPResult
}

func makeMockNMARotateTLSCertsOpHTTPResponse(host string) *mockNMARotateTLSCertsOpHTTPResult {
	return &mockNMARotateTLSCertsOpHTTPResult{
		*makeMockOpResponse(host),
	}
}

func (res *mockNMARotateTLSCertsOpHTTPResult) setSuccess(expectedInfo *tlsConfigInfo) *mockNMARotateTLSCertsOpHTTPResult {
	res.mockOpHTTPResult.setSuccess()
	return res.setResponse(expectedInfo.Digest)
}

func (res *mockNMARotateTLSCertsOpHTTPResult) setResponse(digest string) *mockNMARotateTLSCertsOpHTTPResult {
	resContent := rotateTLSCertsResponse{
		TLSConfigDigest: digest,
	}
	content, err := json.Marshal(resContent)
	if err != nil {
		panic(err) // test problem
	}
	res.content = string(content)
	return res
}

// just some boilerplate to get the op ready to call shouldStopPolling()
func setupMockNMARotateTLSCertsOp(t *testing.T, hosts []string, configCache *tlsConfigInfo) nmaRotateTLSCertsOp {
	vl := vlog.Printer{}
	execContext := makeOpEngineExecContext(vl)
	const username = "someuser"
	const dbname = "somedb"
	const usePW = false
	hostsToSandboxes := make(map[string]string, len(hosts))
	for _, host := range hosts {
		hostsToSandboxes[host] = ""
	}
	opData := getMockNMARotateTLSCertsOpData()
	var nilPW *string
	op, err := makeNMARotateTLSCertsOp(hosts, username, dbname, hostsToSandboxes,
		&opData, AWSSecretManagerType, nilPW, usePW, configCache)
	assert.NoError(t, err)
	op.setupBasicInfo()
	err = op.prepare(&execContext)
	assert.NoError(t, err)
	return op
}

func TestNMARotateTLSCertsOpResult(t *testing.T) {
	const host = "nma_host"
	const extraHost = "nma_extra_host"
	hosts := []string{host, extraHost}
	configCache := &tlsConfigInfo{}

	// test positive case where altering tls config on all hosts produces the same digest
	opSuccess := setupMockNMARotateTLSCertsOp(t, hosts, configCache)
	expectedInfo := tlsConfigInfo{
		Digest:      "the correct digest",
		IsBootstrap: false,
	}
	resColl := &opSuccess.clusterHTTPRequest.ResultCollection
	*resColl = make(map[string]hostHTTPResult, len(hosts))
	(*resColl)[host] = makeMockNMARotateTLSCertsOpHTTPResponse(host).setSuccess(&expectedInfo).hostHTTPResult
	(*resColl)[extraHost] = makeMockNMARotateTLSCertsOpHTTPResponse(extraHost).setSuccess(&expectedInfo).hostHTTPResult
	err := opSuccess.processResult(nil)
	assert.NoError(t, err)
	assert.Equal(t, expectedInfo, *configCache)

	// test that a parsing error results in failure
	opParseError := setupMockNMARotateTLSCertsOp(t, hosts, configCache)
	failureResp := makeMockNMARotateTLSCertsOpHTTPResponse(host).setSuccess(&expectedInfo)
	failureResp.content = "{\"invalid_json\":}}"
	(*resColl)[host] = failureResp.hostHTTPResult
	opParseError.clusterHTTPRequest.ResultCollection = *resColl
	err = opParseError.processResult(nil)
	assert.Error(t, err)

	// test negative case where hosts have mismatched digests
	opExpectMismatch := setupMockNMARotateTLSCertsOp(t, hosts, configCache)
	(*resColl)[host] = makeMockNMARotateTLSCertsOpHTTPResponse(host).setSuccess(&expectedInfo).setResponse("wrong digest").hostHTTPResult
	opExpectMismatch.clusterHTTPRequest.ResultCollection = *resColl
	err = opParseError.processResult(nil)
	assert.Error(t, err)
}
