/*
 (c) Copyright [2023-2025] Open Text.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package vclusterops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

func TestVSetTLSConfig_validateParseOptions(t *testing.T) {
	logger := vlog.Printer{}

	opt := VSetTLSConfigOptionsFactory()
	testDBName := "set_tls_config_test_dbname"
	opt.RawHosts = append(opt.RawHosts, "set-tls-config-test-raw-host")
	opt.DBName = testDBName

	// negative: empty config map
	err := opt.validateParseOptions(logger)
	assert.Error(t, err)

	// negative: empty SecretName
	opt.ServerTLSConfig.SetConfigMap(map[string]string{
		"TLSMode":       "try_verify",
		"SecretManager": "kubernetes",
	})
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)

	// positive
	opt.ServerTLSConfig.SetConfigMap(map[string]string{
		"TLSMode":       "try_verify",
		"SecretManager": "kubernetes",
		"SecretName":    "test-secret",
	})
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)

	assert.Equal(t, opt.ServerTLSConfig.GrantAuth, false, "by default client server's GrantAuth is set to false")
	assert.Equal(t, opt.HTTPSTLSConfig.GrantAuth, true, "by default https' GrantAuth is set to true")

	// negative: only one tls config can set GrantAuth to true.
	opt.ServerTLSConfig.GrantAuth = true
	err = opt.validateParseOptions(logger)
	assert.Error(t, err)

	assert.Equal(t, opt.ServerTLSConfig.ConfigType, ServerTLSKeyPrefix, "client server ConfigType is server")
	assert.Equal(t, opt.HTTPSTLSConfig.ConfigType, HTTPSTLSKeyPrefix, "HTTPS ConfigType is https")
}
