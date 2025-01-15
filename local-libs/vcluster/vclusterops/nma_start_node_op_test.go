package vclusterops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestStartNodeOp(t *testing.T) {
	vl := vlog.Printer{}
	hosts := []string{"host1"}
	startupConf := "/tmp/startup.json"
	// makeNMAStartNodeOp is called by create_db, add_node, start_db.
	// if we pass startupConf to the op initializer, we expect to find
	// it in the data to be sent to nodes/start
	op := makeNMAStartNodeOp(hosts, startupConf)
	op.skipExecute = true
	instructions := []clusterOp{&op}
	clusterOpEngine := makeClusterOpEngine(instructions, nil)

	execContext := makeOpEngineExecContext(vl)
	clusterOpEngine.execContext = &execContext
	execContext.nmaVDatabase = nmaVDatabase{}
	execContext.nmaVDatabase.HostNodeMap = make(map[string]*nmaVNode)
	startCmd := []string{
		"/opt/vertica/bin/vertica",
		"-D",
		"/data/practice_db/v_practice_db_node0001_catalog",
	}
	// this would be normally set by another op. We set it here
	// for testing
	execContext.nmaVDatabase.HostNodeMap[hosts[0]] = &nmaVNode{StartCommand: startCmd}

	err := clusterOpEngine.runWithExecContext(vl, &execContext)
	assert.NoError(t, err)
	httpRequest := op.clusterHTTPRequest.RequestCollection[hosts[0]]
	startNodeData := startNodeRequestData{}
	err = json.Unmarshal([]byte(httpRequest.RequestData), &startNodeData)
	assert.NoError(t, err)
	assert.Equal(t, len(startNodeData.StartCommand), len(startCmd))
	assert.Equal(t, startNodeData.StartupConf, startupConf)
}
