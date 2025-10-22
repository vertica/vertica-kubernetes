package vclusterops

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

const (
	host1      = "host1"
	host2      = "host2"
	host3      = "host3"
	host4      = "host4"
	host5      = "host5"
	totalHosts = 5
)

func makeMockNMAShutdownOpResponse(host string, isErr, isFail, omitMsg, omitErr bool) hostHTTPResult {
	res := hostHTTPResult{}
	res.host = host

	// actual HTTP error
	if isErr {
		res.status = FAILURE
		res.statusCode = InternalErrorCode
		res.err = fmt.Errorf("Server does not exist") // from the NMA 500 return case
		return res
	}

	// success and other shutdown failures are reported with 200 OK
	res.status = SUCCESS
	res.statusCode = SuccessCode
	res.err = nil

	// construct potential return contents.
	contentMap := map[string]string{"shutdown_scheduled": "blahblahblah"}
	if isFail {
		if !omitMsg {
			contentMap[nmaShutdownOpMsgKey] = nmaShutdownOpMsgFail
		}
		if !omitErr {
			contentMap[nmaShutdownOpErrorKey] = "some error message"
		}
	} else {
		if !omitMsg {
			contentMap[nmaShutdownOpMsgKey] = nmaShutdownOpMsgSuccess
		}
		if !omitErr {
			contentMap[nmaShutdownOpErrorKey] = "Null"
		}
	}
	contentJSON, err := json.Marshal(contentMap)
	if err != nil {
		panic(err)
	}
	res.content = string(contentJSON)
	return res
}

func successTest(op *nmaShutdownOp, t *testing.T) {
	vl := vlog.Printer{}
	execContext := makeOpEngineExecContext(vl)

	// test positive case of successful shutdown
	op.setupBasicInfo()
	err := op.prepare(&execContext)
	assert.NoError(t, err)
	resColl := &op.clusterHTTPRequest.ResultCollection
	*resColl = make(map[string]hostHTTPResult, totalHosts)
	(*resColl)[host1] = makeMockNMAShutdownOpResponse(host1, false, false, false, false)
	err = op.processResult(&execContext)
	assert.NoError(t, err)
}

func negativeTest(op *nmaShutdownOp, msgType string, t *testing.T) {
	vl := vlog.Printer{}
	execContext := makeOpEngineExecContext(vl)

	op.setupBasicInfo()
	err := op.prepare(&execContext)
	assert.NoError(t, err)
	resColl := &op.clusterHTTPRequest.ResultCollection
	*resColl = make(map[string]hostHTTPResult, totalHosts)
	(*resColl)[host1] = makeMockNMAShutdownOpResponse(host1, false, false, false, false)
	// test composite negative case
	// 1 successful shutdown (already added)
	// 1 error case
	(*resColl)[host2] = makeMockNMAShutdownOpResponse(host2, true /*isErr*/, false, false, false)
	// 1 missing message case
	(*resColl)[host3] = makeMockNMAShutdownOpResponse(host3, false, false, true /*omitMsg*/, false)
	// 1 missing error case
	(*resColl)[host4] = makeMockNMAShutdownOpResponse(host4, false, true /*isFail */, false, true /*omitErr*/)
	// 1 shutdown failure case
	(*resColl)[host5] = makeMockNMAShutdownOpResponse(host5, false, true /*isFail */, false, false)

	err = op.processResult(&execContext)
	assert.Error(t, err)

	// check that we have the right error count, and since map range is ND order,
	// check the concatened error string for the messages
	errs := strings.Split(err.Error(), "\n")
	assert.Len(t, errs, 4)
	assert.Contains(t, err.Error(), "does not exist")
	assert.Contains(t, err.Error(), op.makeMissingFieldError(nmaShutdownOpMsgKey).Error())
	assert.Contains(t, err.Error(), op.makeMissingFieldError(nmaShutdownOpErrorKey).Error())
	assert.Contains(t, err.Error(), "nma "+msgType+" failed, details: some error")
}

func TestNMAShutdownOp(t *testing.T) {
	// test positive case of successful shutdown
	op := makeNMAShutdownOp([]string{host1})
	successTest(&op, t)

	op = makeNMAShutdownOp([]string{host1, host2, host3, host4, host5})
	negativeTest(&op, "shutdown", t)
}

func TestNMARestartOp(t *testing.T) {
	// test positive case of successful shutdown
	op := makeNMARestartOp([]string{host1})
	successTest(&op, t)

	op = makeNMARestartOp([]string{host1, host2, host3, host4, host5})
	negativeTest(&op, "restart", t)
}
