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
	opData := RotateTLSCertsData{
		KeySecretName:    "key",
		KeyConfig:        "key_config",
		CertSecretName:   "cert",
		CertConfig:       "cert_config",
		CACertSecretName: "ca_cert",
		CACertConfig:     "ca_cert_config",
		TLSMode:          "try_verify",
		TLSConfig:        "HTTPS",
	}

	// construction should succeed
	op, err := makeNMARotateTLSCertsOp(hosts, username, dbName, hostsToSandboxes,
		&opData, AWSSecretManagerType, &pwStr, usePW)
	assert.NoError(t, err)

	// run through prepare() phase only
	op.skipExecute = true
	instructions := []clusterOp{&op}
	log := vlog.Printer{}
	clusterOpEngine := makeClusterOpEngine(instructions, nil)
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
