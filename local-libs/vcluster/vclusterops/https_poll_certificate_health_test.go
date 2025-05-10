package vclusterops

import (
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

	// construction should succeed
	op, err := makeHTTPSPollCertificateHealthOp(hosts, usePW, username, &pwStr)
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
		assert.Equal(t, "v1/health", httpRequest.Endpoint)
		assert.Equal(t, GetMethod, httpRequest.Method)
		assert.Equal(t, defaultHTTPSRequestTimeoutSeconds, httpRequest.Timeout)
		assert.Empty(t, httpRequest.Username)
		assert.Nil(t, httpRequest.Password)
	}
}

// mockHTTPSPollCertHealthOpHTTPResult has nearly the same format as the NMA version
type mockHTTPSPollCertHealthOpHTTPResult struct {
	mockNMAPollCertHealthOpHTTPResult
}

func makeMockHTTPSPollCertHealthOpResponse(host string) *mockHTTPSPollCertHealthOpHTTPResult {
	return &mockHTTPSPollCertHealthOpHTTPResult{
		*makeMockNMAPollCertHealthOpResponse(host),
	}
}

func (res *mockHTTPSPollCertHealthOpHTTPResult) setSuccess() *mockHTTPSPollCertHealthOpHTTPResult {
	res.mockNMAPollCertHealthOpHTTPResult.setSuccess()
	res.content = "" // only difference is https health endpoint doesn't have a body
	return res
}

// just some boilerplate to get the op ready to call shouldStopPolling()
func setupMockHTTPSPollCertificateHealthOp(t *testing.T, hosts []string) httpsPollCertificateHealthOp {
	vl := vlog.Printer{}
	execContext := makeOpEngineExecContext(vl)
	const username = "someuser"
	const usePW = false
	var nilPW *string
	op, err := makeHTTPSPollCertificateHealthOp(hosts, usePW, username, nilPW)
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

	// test positive case of https certs passing
	opSuccess := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	resColl := &opSuccess.clusterHTTPRequest.ResultCollection
	*resColl = make(map[string]hostHTTPResult, len(hosts))
	(*resColl)[host] = makeMockHTTPSPollCertHealthOpResponse(host).setSuccess().hostHTTPResult
	(*resColl)[extraHost] = makeMockHTTPSPollCertHealthOpResponse(extraHost).setSuccess().hostHTTPResult
	doStop, err := opSuccess.shouldStopPolling()
	assert.NoError(t, err)
	assert.True(t, doStop)

	// test that an error unrelated to where we are in the restart process is fatal
	opError := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	(*resColl)[host] = makeMockHTTPSPollCertHealthOpResponse(host).setFailure().hostHTTPResult
	opError.clusterHTTPRequest.ResultCollection = *resColl
	_, err = opError.shouldStopPolling()
	assert.Error(t, err)

	// test a potential client-side TLS error indicating the HTTPS service hasn't restarted with the right certs yet
	opException := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	(*resColl)[host] = makeMockHTTPSPollCertHealthOpResponse(host).setException().hostHTTPResult
	opException.clusterHTTPRequest.ResultCollection = *resColl
	doStop, err = opException.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)

	// test the server-side error indicating HTTPS hasn't restarted with the right certs yet
	opUnauthorized := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	(*resColl)[host] = makeMockHTTPSPollCertHealthOpResponse(host).setUnauthorized().hostHTTPResult
	opUnauthorized.clusterHTTPRequest.ResultCollection = *resColl
	doStop, err = opUnauthorized.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)

	// test a reset connection when the HTTPS server dies abruptly during an endpoint call
	opEOF := setupMockHTTPSPollCertificateHealthOp(t, hosts)
	(*resColl)[host] = makeMockHTTPSPollCertHealthOpResponse(host).setEOF().hostHTTPResult
	opException.clusterHTTPRequest.ResultCollection = *resColl
	doStop, err = opEOF.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)
}
