package vclusterops

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

//nolint:gosec // test uses "hardcoded credentials"
func TestNMASetTLSOp(t *testing.T) {
	const (
		kubernetesSecretManager = "kubernetes"
		awsSecretManager        = "AWS"
		tlsMode                 = "try-verify"
		userName                = "test_tls_user"
		dbName                  = "test_tls_db"
		namespace               = "test_ns"
		secretName              = "test_secret"
		caDataKey               = "test_ca_data_key"
		certDataKey             = "test_cert_data_key"
		keyDataKey              = "test_key_data_key"
		region                  = "us-east-1"
		versionID               = "v1"
	)
	hosts := []string{"host1"}
	pwStr := "test_tls_pw_str"

	baseOptions := VCreateDatabaseOptions{
		DatabaseOptions: DatabaseOptions{
			UserName:    userName,
			DBName:      dbName,
			Password:    &pwStr,
			usePassword: true,
			Hosts:       hosts,
		},
	}

	commonTLSConfig := map[string]string{
		TLSSecretManagerKeySecretName:    secretName,
		TLSSecretManagerKeyCACertDataKey: caDataKey,
		TLSSecretManagerKeyCertDataKey:   certDataKey,
		TLSSecretManagerKeyKeyDataKey:    keyDataKey,
		TLSSecretManagerKeyTLSMode:       tlsMode,
	}

	runTLSOp := func(tlsConfig map[string]string, validate func(data nmaSetTLSRequestData)) {
		baseOptions.ServerTLSConfiguration = tlsConfig
		op, err := makeNMASetTLSOp(&baseOptions.DatabaseOptions, serverTLSKeyPrefix, true, true, tlsConfig)
		assert.NoError(t, err)

		op.skipExecute = true
		engine := makeClusterOpEngine([]clusterOp{&op}, nil)
		assert.NoError(t, engine.run(vlog.Printer{}))

		for _, host := range hosts {
			req := op.clusterHTTPRequest.RequestCollection[host]
			assert.Equal(t, "v1/vertica/tls", req.Endpoint)
			assert.Equal(t, PutMethod, req.Method)

			var data nmaSetTLSRequestData
			err := json.Unmarshal([]byte(req.RequestData), &data)
			assert.NoError(t, err)
			validate(data)
		}
	}

	// Kubernetes secret manager test
	k8sConfig := cloneMap(commonTLSConfig)
	k8sConfig[TLSSecretManagerKeySecretManager] = kubernetesSecretManager
	k8sConfig[TLSSecretManagerKeyNamespace] = namespace

	runTLSOp(k8sConfig, func(data nmaSetTLSRequestData) {
		assert.Equal(t, kubernetesSecretManager, data.TLSSecretManager)
		assert.Equal(t, namespace, data.TLSNamespace)
		assert.Equal(t, secretName, data.TLSSecretName)
		assert.Equal(t, keyDataKey, data.TLSKeyDataKey)
		assert.Equal(t, certDataKey, data.TLSCertDataKey)
		assert.Equal(t, caDataKey, data.TLSCADataKey)
		assert.Equal(t, tlsMode, data.TLSMode)
		assert.Equal(t, userName, data.DBUsername)
		assert.Equal(t, pwStr, data.DBPassword)
		assert.Equal(t, dbName, data.DBName)
	})

	// AWS secret manager test
	awsConfig := cloneMap(commonTLSConfig)
	awsConfig[TLSSecretManagerKeySecretManager] = awsSecretManager
	awsConfig[TLSSecretManagerKeyAWSRegion] = region
	awsConfig[TLSSecretManagerKeyAWSSecretVersionID] = versionID

	runTLSOp(awsConfig, func(data nmaSetTLSRequestData) {
		assert.Equal(t, awsSecretManager, data.TLSSecretManager)
		assert.Equal(t, region, data.AWSRegion)
		assert.Equal(t, versionID, data.AWSSecretVersionID)
	})
}
