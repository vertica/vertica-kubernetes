package vclusterops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

//nolint:gosec // test uses "hardcoded credentials"
func TestNMASetTLSOp(t *testing.T) {
	const KubernetesSecretManagerName string = "KubernetesSecretManager"
	const TLSMode string = "TLSMode"
	const CADataKey string = "CADataKey"
	const CertDataKey string = "CertDataKey"
	const KeyDataKey string = "KeyDataKey"
	hosts := []string{"host1"}
	pwStr := "test_tls_pw_str"
	userName := "test_tls_user"
	dbName := "test_tls_db"
	namespace := "test_ns"
	secretName := "test_secret"
	caDataKey := "test_ca_data_key"
	certDataKey := "test_cert_data_key"
	keyDataKey := "test_key_data_key"
	tlsMode := "try-verify"
	options := VCreateDatabaseOptions{
		DatabaseOptions: DatabaseOptions{
			UserName:    userName,
			DBName:      dbName,
			Password:    &pwStr,
			usePassword: true,
			Hosts:       hosts,
		},
		ServerTLSConfiguration: map[string]string{
			"Namespace":     namespace,
			"SecretManager": KubernetesSecretManagerName,
			"SecretName":    secretName,
			CADataKey:       caDataKey,
			CertDataKey:     certDataKey,
			KeyDataKey:      keyDataKey,
			TLSMode:         tlsMode,
		},
	}

	// construction should succeed
	op, err := makeNMASetTLSOp(&options.DatabaseOptions, serverTLSKeyPrefix, true, true, options.ServerTLSConfiguration)
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
		assert.Equal(t, "v1/vertica/tls", httpRequest.Endpoint)
		assert.Equal(t, PutMethod, httpRequest.Method)
		data := nmaSetTLSRequestData{}
		err = json.Unmarshal([]byte(httpRequest.RequestData), &data)

		assert.NoError(t, err)
		assert.Equal(t, userName, data.DBUsername)
		assert.Equal(t, pwStr, data.DBPassword)
		assert.Equal(t, dbName, data.DBName)
		assert.Equal(t, KubernetesSecretManagerName, data.TLSSecretManager)
		assert.Equal(t, namespace, data.TLSNamespace)
		assert.Equal(t, secretName, data.TLSSecretName)
		assert.Equal(t, keyDataKey, data.TLSKeyDataKey)
		assert.Equal(t, certDataKey, data.TLSCertDataKey)
		assert.Equal(t, caDataKey, data.TLSCADataKey)
		assert.Equal(t, tlsMode, data.TLSMode)
	}
}
