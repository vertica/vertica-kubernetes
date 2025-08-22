package vclusterops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestHTTPSPollCertificateHealthOp(t *testing.T) {
	// make a mock op
	host1 := "h1"
	host2 := "h2"
	host3 := "h3"
	hosts := []string{host1, host2, host3}
	username := "test_username"
	pwStr := "test_pw_string"
	usePW := false
	config := &tlsConfigInfo{}

	// construction should succeed
	op, err := makeHTTPSPollCertificateHealthOp(hosts, config, usePW, username, &pwStr)
	assert.NoError(t, err)
	pollingTimeout := op.getPollingTimeout()
	assert.Equal(t, util.GetEnvInt("NODE_STATE_POLLING_TIMEOUT", StartupPollingTimeout), pollingTimeout)

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
		assert.Equal(t, "v1/tls/info", httpRequest.Endpoint)
		assert.Equal(t, GetMethod, httpRequest.Method)
		assert.Equal(t, defaultHTTPSRequestTimeoutSeconds, httpRequest.Timeout)
		assert.Empty(t, httpRequest.Username)
		assert.Nil(t, httpRequest.Password)
	}
}

// mockHTTPSPollCertHealthOpHTTPResult has nearly the same format as the NMA version
type mockHTTPSPollCertHealthOpHTTPResult struct {
	mockOpHTTPResult
}

func makeMockHTTPSPollCertHealthOpResponse(host string) *mockHTTPSPollCertHealthOpHTTPResult {
	return &mockHTTPSPollCertHealthOpHTTPResult{
		*makeMockOpResponse(host),
	}
}

func (res *mockHTTPSPollCertHealthOpHTTPResult) setSuccess(op *httpsPollCertificateHealthOp) *mockHTTPSPollCertHealthOpHTTPResult {
	res.mockOpHTTPResult.setSuccess()
	return res.setResponse(op.expectedTLSConfigInfo)
}

func (res *mockHTTPSPollCertHealthOpHTTPResult) setResponse(resInfo *tlsConfigInfo) *mockHTTPSPollCertHealthOpHTTPResult {
	content, err := json.Marshal(resInfo)
	if err != nil {
		panic(err) // test problem
	}
	res.content = string(content)
	return res
}

// just some boilerplate to get the op ready to call shouldStopPolling()
func setupMockHTTPSPollCertificateHealthOp(t *testing.T, hosts []string) httpsPollCertificateHealthOp {
	vl := vlog.Printer{}
	execContext := makeOpEngineExecContext(vl)
	const username = "someuser"
	const usePW = false
	var nilPW *string
	config := &tlsConfigInfo{
		Digest: "deadbeef0123",
	}
	op, err := makeHTTPSPollCertificateHealthOp(hosts, config, usePW, username, nilPW)
	assert.NoError(t, err)
	op.setupBasicInfo()
	err = op.prepare(&execContext)
	assert.NoError(t, err)
	return op
}

func TestHTTPSPollCertificateHealthOpPolling(t *testing.T) {
	const host = "https_host"
	const extraHost = "https_extra_host"
	hosts := []string{host, extraHost}

	// test positive case of simple rotation success
	opSuccess := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	resColl := &opSuccess.clusterHTTPRequest.ResultCollection
	*resColl = make(map[string]hostHTTPResult, len(hosts))
	(*resColl)[host] = makeMockHTTPSPollCertHealthOpResponse(host).setSuccess(&opSuccess).hostHTTPResult
	(*resColl)[extraHost] = makeMockHTTPSPollCertHealthOpResponse(extraHost).setSuccess(&opSuccess).hostHTTPResult
	doStop, err := opSuccess.shouldStopPolling()
	assert.NoError(t, err)
	assert.True(t, doStop)

	// test that a host having a bootstrap version of the expected config results in continued polling
	opExpectBootstrap := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	bootstrapResp := *opExpectBootstrap.expectedTLSConfigInfo
	bootstrapResp.IsBootstrap = true
	(*resColl)[host] = makeMockHTTPSPollCertHealthOpResponse(host).setSuccess(&opExpectBootstrap).setResponse(&bootstrapResp).hostHTTPResult
	opExpectBootstrap.clusterHTTPRequest.ResultCollection = *resColl
	doStop, err = opExpectBootstrap.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)

	// test that a host reporting a different digest than expected results in continued polling
	opExpectMismatch := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	mismatchResp := *opExpectMismatch.expectedTLSConfigInfo
	mismatchResp.Digest = "abcd1234"
	(*resColl)[host] = makeMockHTTPSPollCertHealthOpResponse(host).setSuccess(&opExpectMismatch).setResponse(&mismatchResp).hostHTTPResult
	opExpectMismatch.clusterHTTPRequest.ResultCollection = *resColl
	doStop, err = opExpectMismatch.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)

	// test that a parsing error results in immediate failure
	opParseError := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	failureResp := makeMockHTTPSPollCertHealthOpResponse(host).setSuccess(&opParseError)
	failureResp.content = "{{\"invalid_json\":}"
	(*resColl)[host] = failureResp.hostHTTPResult
	opParseError.clusterHTTPRequest.ResultCollection = *resColl
	_, err = opParseError.shouldStopPolling()
	assert.Error(t, err)

	// test the various cases where status != SUCCESS
	httpsPollCertificateHealthOpPollingErrorCases(t, hosts, host, *resColl)
}

func httpsPollCertificateHealthOpPollingErrorCases(t *testing.T, hosts []string, host string, resColl map[string]hostHTTPResult) {
	// test that an error unrelated to where we are in the restart process is fatal
	opError := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	resColl[host] = makeMockHTTPSPollCertHealthOpResponse(host).setFailure().hostHTTPResult
	opError.clusterHTTPRequest.ResultCollection = resColl
	_, err := opError.shouldStopPolling()
	assert.Error(t, err)

	// test a potential client-side TLS error indicating the HTTPS service hasn't restarted with the right certs yet
	opException := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	resColl[host] = makeMockHTTPSPollCertHealthOpResponse(host).setException().hostHTTPResult
	opException.clusterHTTPRequest.ResultCollection = resColl
	doStop, err := opException.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)

	// test the server-side error indicating HTTPS hasn't restarted with the right certs yet
	opUnauthorized := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	resColl[host] = makeMockHTTPSPollCertHealthOpResponse(host).setUnauthorized().hostHTTPResult
	opUnauthorized.clusterHTTPRequest.ResultCollection = resColl
	doStop, err = opUnauthorized.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)

	// test a reset connection when the HTTPS server dies abruptly during an endpoint call
	opEOF := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	resColl[host] = makeMockHTTPSPollCertHealthOpResponse(host).setEOF().hostHTTPResult
	opException.clusterHTTPRequest.ResultCollection = resColl
	doStop, err = opEOF.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)
}
