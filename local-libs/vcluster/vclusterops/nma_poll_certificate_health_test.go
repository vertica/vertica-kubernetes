package vclusterops

import (
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

// mockOpHTTPResult exists to prevent IDE autocomplete from cluttering
type mockOpHTTPResult struct {
	hostHTTPResult
}

func makeMockOpResponse(host string) *mockOpHTTPResult {
	res := mockOpHTTPResult{}
	res.host = host
	return &res
}

func (res *mockOpHTTPResult) setSuccess() *mockOpHTTPResult {
	res.status = SUCCESS
	res.statusCode = SuccessCode
	return res
}

func (res *mockOpHTTPResult) setFailure() *mockOpHTTPResult {
	res.status = FAILURE
	res.statusCode = InternalErrorCode
	res.err = fmt.Errorf("something's always wrong")
	return res
}

func (res *mockOpHTTPResult) setException() *mockOpHTTPResult {
	res.status = EXCEPTION
	res.err = fmt.Errorf("tls: peer cert isn't worth the bytes it's written on")
	return res
}

func (res *mockOpHTTPResult) setEOF() *mockOpHTTPResult {
	res.status = EOFEXCEPTION
	res.err = io.EOF
	return res
}

func (res *mockOpHTTPResult) setUnauthorized() *mockOpHTTPResult {
	res.status = FAILURE
	res.statusCode = UnauthorizedCode
	res.err = fmt.Errorf("peer rejects our rotten old certs")
	return res
}

type mockNMAPollCertHealthOpHTTPResult struct {
	mockOpHTTPResult
}

func makeMockNMAPollCertHealthOpResponse(host string) *mockNMAPollCertHealthOpHTTPResult {
	return &mockNMAPollCertHealthOpHTTPResult{
		*makeMockOpResponse(host),
	}
}

func (res *mockNMAPollCertHealthOpHTTPResult) setSuccess() *mockNMAPollCertHealthOpHTTPResult {
	res.mockOpHTTPResult.setSuccess()
	res.content = `{"healthy":"true"}`
	return res
}

//nolint:funlen // this is not worth decomposing
func TestNMAPollCertHealthOp(t *testing.T) {
	vl := vlog.Printer{}
	execContext := makeOpEngineExecContext(vl)
	const host = "host"
	const extraHost = "extra_host"
	hosts := []string{host, extraHost}

	// test positive case of nma certs passing
	opSuccess := makeNMAPollCertHealthOp(hosts, 0)
	opSuccess.setupBasicInfo()
	err := opSuccess.prepare(&execContext)
	assert.NoError(t, err)
	resColl := &opSuccess.clusterHTTPRequest.ResultCollection
	*resColl = make(map[string]hostHTTPResult, len(hosts))
	(*resColl)[host] = makeMockNMAPollCertHealthOpResponse(host).setSuccess().hostHTTPResult
	(*resColl)[extraHost] = makeMockNMAPollCertHealthOpResponse(extraHost).setSuccess().hostHTTPResult
	doStop, err := opSuccess.shouldStopPolling()
	assert.NoError(t, err)
	assert.True(t, doStop)

	// test that error unrelated to where we are in the restart process is fatal
	opError := makeNMAPollCertHealthOp(hosts, 0)
	opError.setupBasicInfo()
	err = opError.prepare(&execContext)
	assert.NoError(t, err)
	(*resColl)[host] = makeMockNMAPollCertHealthOpResponse(host).setFailure().hostHTTPResult
	opError.clusterHTTPRequest.ResultCollection = *resColl
	_, err = opError.shouldStopPolling()
	assert.Error(t, err)

	// test a potential client-side TLS error indicating NMA hasn't restarted with the right certs yet
	opException := makeNMAPollCertHealthOp(hosts, 0)
	opException.setupBasicInfo()
	err = opException.prepare(&execContext)
	assert.NoError(t, err)
	(*resColl)[host] = makeMockNMAPollCertHealthOpResponse(host).setException().hostHTTPResult
	opException.clusterHTTPRequest.ResultCollection = *resColl
	doStop, err = opException.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)

	// test the server-side error indicating NMA hasn't restarted with the right certs yet
	opUnauthorized := makeNMAPollCertHealthOp(hosts, 0)
	opUnauthorized.setupBasicInfo()
	err = opUnauthorized.prepare(&execContext)
	assert.NoError(t, err)
	(*resColl)[host] = makeMockNMAPollCertHealthOpResponse(host).setUnauthorized().hostHTTPResult
	opUnauthorized.clusterHTTPRequest.ResultCollection = *resColl
	doStop, err = opUnauthorized.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)

	// test a reset connection when the NMA server dies abruptly during an endpoint call
	opEOF := makeNMAPollCertHealthOp(hosts, 0)
	opEOF.setupBasicInfo()
	err = opEOF.prepare(&execContext)
	assert.NoError(t, err)
	(*resColl)[host] = makeMockNMAPollCertHealthOpResponse(host).setEOF().hostHTTPResult
	opException.clusterHTTPRequest.ResultCollection = *resColl
	doStop, err = opEOF.shouldStopPolling()
	assert.NoError(t, err)
	assert.False(t, doStop)
}
