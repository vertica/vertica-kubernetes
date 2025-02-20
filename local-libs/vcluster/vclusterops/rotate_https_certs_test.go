package vclusterops

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/util"
)

type mockRotateHTTPSCertsVnodes struct {
	nodes            []*VCoordinationNode
	hosts            []string
	hostsToSandboxes map[string]string
}

func (vnodes *mockRotateHTTPSCertsVnodes) addHost(sandbox, status string) {
	vnode := makeVCoordinationNode()
	index := len(vnodes.hosts)
	vnode.Name = fmt.Sprintf("vnode%d", index)
	vnode.Address = fmt.Sprintf("192.168.0.%d", index)
	vnode.Sandbox = sandbox
	vnode.State = status
	vnodes.nodes = append(vnodes.nodes, &vnode)
	vnodes.hosts = append(vnodes.hosts, vnode.Address)
	vnodes.hostsToSandboxes[vnode.Address] = vnode.Sandbox
}

func (vnodes *mockRotateHTTPSCertsVnodes) addUpHost(sandbox string) {
	vnodes.addHost(sandbox, util.NodeUpState)
}

func (vnodes *mockRotateHTTPSCertsVnodes) addDownHost(sandbox string) {
	vnodes.addHost(sandbox, util.NodeDownState)
}

// makeMockRotateHTTPSCertsVnodes adds one down node per sandbox + main cluster, and if the sandbox
// is not excluded, two up nodes as well
func makeMockRotateHTTPSCertsVnodes(sandboxes []string, allDownSandboxes ...string) mockRotateHTTPSCertsVnodes {
	vnodes := mockRotateHTTPSCertsVnodes{hostsToSandboxes: map[string]string{}}
	for _, sandbox := range sandboxes {
		vnodes.addDownHost(sandbox)
		if !slices.Contains(allDownSandboxes, sandbox) {
			vnodes.addUpHost(sandbox)
			vnodes.addUpHost(sandbox)
		}
	}
	return vnodes
}

func (vnodes *mockRotateHTTPSCertsVnodes) makeVDB() *VCoordinationDatabase {
	vdb := makeVCoordinationDatabase()
	vdb.HostNodeMap = makeVHostNodeMap()
	for _, vnode := range vnodes.nodes {
		err := vdb.addNode(vnode)
		if err != nil {
			panic(err) // indicates test issue
		}

		if vnode.Sandbox != "" && !slices.Contains(vdb.AllSandboxes, vnode.Sandbox) {
			vdb.AllSandboxes = append(vdb.AllSandboxes, vnode.Sandbox)
		}
	}
	return &vdb
}

func (vnodes *mockRotateHTTPSCertsVnodes) makeOptions() *VRotateHTTPSCertsOptions {
	opts := VRotateHTTPSCertsOptionsFactory()
	opts.Hosts = vnodes.hosts
	return &opts
}

//nolint:dogsled // doesn't like _, _, _, but we only care about the errors here
func TestRotateHTTPSCertsGetVDBInfo(t *testing.T) {
	// fake cluster info
	mc := ""
	sb1 := "sand_A"
	sb2 := "sand_B"
	sandboxes := []string{mc, sb1, sb2}

	// UP host present in each sb + main cluster -> success
	vnodes := makeMockRotateHTTPSCertsVnodes(sandboxes)
	opts := vnodes.makeOptions()
	vdb := vnodes.makeVDB()
	upHosts, initiatorHosts, hostsToSandboxes, err := opts.getVDBInfo(vdb)

	assert.NoError(t, err)
	assert.Len(t, upHosts, 2*len(sandboxes))
	assert.Len(t, initiatorHosts, len(sandboxes))
	assert.Len(t, hostsToSandboxes, len(vnodes.hostsToSandboxes))
	for host, sandbox := range hostsToSandboxes {
		assert.Equal(t, vnodes.hostsToSandboxes[host], sandbox)
	}

	// no UP host in main cluster -> failure
	vnodes = makeMockRotateHTTPSCertsVnodes(sandboxes, mc)
	opts = vnodes.makeOptions()
	vdb = vnodes.makeVDB()
	_, _, _, err = opts.getVDBInfo(vdb)

	assert.ErrorContains(t, err, "main cluster")

	// no UP host in sandbox -> failure
	vnodes = makeMockRotateHTTPSCertsVnodes(sandboxes, sb1)
	opts = vnodes.makeOptions()
	vdb = vnodes.makeVDB()
	_, _, _, err = opts.getVDBInfo(vdb)

	assert.ErrorContains(t, err, "sandbox")
}
