package vclusterops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

//nolint:gosec // test uses "hardcoded credentials"
func TestNMARotateHTTPSCertsOp(t *testing.T) {
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
	opData := RotateHTTPSCertsData{
		KeySecretName:    "key",
		KeyConfig:        "key_config",
		CertSecretName:   "cert",
		CertConfig:       "cert_config",
		CACertSecretName: "ca_cert",
		CACertConfig:     "ca_cert_config",
		TLSMode:          "try_verify",
	}

	// construction should succeed
	op, err := makeNMARotateHTTPSCertsOp(hosts, username, dbName, hostsToSandboxes,
		&opData, &pwStr, usePW)
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
		assert.Equal(t, "v1/vertica/https/rotate-certs", httpRequest.Endpoint)
		assert.Equal(t, PostMethod, httpRequest.Method)
		data := rotateHTTPSCertsData{}
		err = json.Unmarshal([]byte(httpRequest.RequestData), &data)

		assert.NoError(t, err)
		assert.Equal(t, username, data.DBUsername)
		assert.Equal(t, pwStr, data.DBPassword)
		assert.Equal(t, dbName, data.DBName)
		assert.Equal(t, opData, data.RotateHTTPSCertsData)
		assert.Equal(t, "KubernetesSecretManager", data.SecretManager)
	}
}
