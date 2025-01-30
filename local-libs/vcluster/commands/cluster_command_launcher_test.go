/*
 (c) Copyright [2023-2024] Open Text.
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

package commands

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigPathDefaults(t *testing.T) {
	const envConfigPath = "/scratch_b/mspilchen/cfg-from-env.yaml"
	const optsConfigPath = "/scratch_b/mspilchen/cfg-from-opts.yaml"

	// Make sure option flag takes precedence
	os.Setenv(vclusterConfigEnv, envConfigPath)
	dbOptions.ConfigPath = optsConfigPath
	initConfig()
	assert.Equal(t, dbOptions.ConfigPath, optsConfigPath)

	// Use environment variable if option flag isn't set
	dbOptions.ConfigPath = ""
	initConfig()
	assert.Equal(t, dbOptions.ConfigPath, envConfigPath)

	// Pick a sane default if neither option flag or env is set. There are two
	// cases: vcluster is run from /opt/vertica/bin and when it isn't.
	dbOptions.ConfigPath = ""
	os.Setenv(vclusterConfigEnv, "")
	initConfigImpl("/opt/vertica/bin/vcluster",
		false, /* do not ensure /opt/vertica/config exists */
		false /* do not create user config dir */)
	var optVerticaConfigPath = fmt.Sprintf("/opt/vertica/config/%s", defConfigFileName)
	assert.Contains(t, dbOptions.ConfigPath, optVerticaConfigPath)

	// Pick a sane default if neither option flag or env is set.
	dbOptions.ConfigPath = ""
	os.Setenv(vclusterConfigEnv, "")
	initConfigImpl("/usr/bin/vcluster",
		false, /* do not ensure /opt/vertica/config exists */
		false /* do not create user config dir */)
	var homeConfigPathPartial = fmt.Sprintf("vcluster/%s", defConfigFileName)
	assert.Contains(t, dbOptions.ConfigPath, homeConfigPathPartial)
}

type mockOperatingSystem struct {
	mockExecutablePath    string
	mockExecutablePathErr error
	mockUserConfigDir     string
	mockUserConfigDirErr  error
	mockMkdirAllErr       error
}

func (m *mockOperatingSystem) Executable() (string, error) {
	return m.mockExecutablePath, m.mockExecutablePathErr
}

func (m *mockOperatingSystem) UserConfigDir() (string, error) {
	return m.mockUserConfigDir, m.mockUserConfigDirErr
}

func (m *mockOperatingSystem) MkdirAll(_ string, _ os.FileMode) error {
	return m.mockMkdirAllErr
}
func TestSetLogPathImpl(t *testing.T) {
	mockOpsys := mockOperatingSystem{
		mockExecutablePath:    defaultExecutablePath,
		mockExecutablePathErr: nil,
		mockUserConfigDir:     "/home/user/.config",
		mockUserConfigDirErr:  nil,
		mockMkdirAllErr:       nil,
	}

	logPath := setLogPathImpl(&mockOpsys)
	expectedLogPath := defaultLogPath
	assert.Equal(t, expectedLogPath, logPath)

	mockOpsys.mockExecutablePath = "/usr/bin/vcluster"
	logPath = setLogPathImpl(&mockOpsys)
	defaultHomeConfigDirLogPath := "/home/user/.config/vcluster/vcluster.log"
	expectedLogPath = defaultHomeConfigDirLogPath
	assert.Equal(t, expectedLogPath, logPath)

	mockOpsys.mockExecutablePathErr = fmt.Errorf("error getting executable path")
	logPath = setLogPathImpl(&mockOpsys)
	expectedLogPath = defaultLogPath
	assert.Equal(t, expectedLogPath, logPath)

	mockOpsys.mockExecutablePathErr = nil
	mockOpsys.mockUserConfigDirErr = fmt.Errorf("error getting user config directory")
	logPath = setLogPathImpl(&mockOpsys)
	assert.Equal(t, expectedLogPath, logPath)

	mockOpsys.mockUserConfigDirErr = nil
	mockOpsys.mockMkdirAllErr = fmt.Errorf("error creating config directory")
	logPath = setLogPathImpl(&mockOpsys)
	expectedLogPath = defaultHomeConfigDirLogPath
	assert.Equal(t, expectedLogPath, logPath)
}
