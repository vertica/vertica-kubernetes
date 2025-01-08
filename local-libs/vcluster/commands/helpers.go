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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
	"golang.org/x/term"
)

const kubernetesPort = "KUBERNETES_PORT"

func readDBPasswordFromPrompt() (string, error) {
	// Prompt the user to enter the password
	fmt.Print("Enter password: ")

	// Disable echoing
	passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	fmt.Println()
	return string(passwordBytes), nil
}

func readFromStdin() (string, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read password from STDIN: %w", err)
	}
	return string(data), nil
}

func isK8sEnvironment() bool {
	port, portSet := os.LookupEnv(kubernetesPort)
	return portSet && port != ""
}

// this function validates that the connection file path is absolute and ends with yaml or yml
func validateYamlFilePath(connFile string, logger vlog.Printer) error {
	if !filepath.IsAbs(connFile) {
		filePathError := errors.New(
			"Invalid connection file path: " + globals.connFile + ". The connection file path must be absolute.")
		logger.Error(filePathError, "Connection file path error:")
		return filePathError
	}
	ext := filepath.Ext(connFile)
	if ext != dotYaml && ext != dotYml {
		fileTypeError := errors.New("Invalid file type: " + ext + ". Only .yaml or .yml is allowed.")
		logger.Error(fileTypeError, "Connection file type error:")
		return fileTypeError
	}
	return nil
}

// this function translates raw error messages into more user friendly error message
func converErrorMessage(err error, logger vlog.Printer) string {
	errMsg := err.Error()
	logger.Error(err, "error to be converted into err msg")
	if strings.Contains(errMsg, "down database") {
		return "failed to vertify connection parameters. please check your db name and host list"
	} else if strings.Contains(errMsg, "Wrong password") {
		return "failed to vertify connection parameters. please check your db username and password"
	} else if strings.Contains(errMsg, "rather than database") {
		return "failed to vertify connection parameters. please check your db name"
	} else if strings.Contains(errMsg, "no such host") || strings.Contains(errMsg, "network is unreachable") ||
		strings.Contains(errMsg, "fail to send request") || strings.Contains(errMsg, "server misbehaving") ||
		strings.Contains(errMsg, "i/o timeout") {
		return "failed to vertify connection parameters. please check your host list"
	}
	return "failed to vertify connection parameters: " + errMsg
}

// this function calls ClusterCommand.FetchNodesDetails() for each input hosts and return both valid and invalid hosts
func fetchNodeDetails(vcc vclusterops.ClusterCommands, fetchNodeDetailsOptions *vclusterops.VFetchNodesDetailsOptions) (validHosts []string,
	invalidHosts []string, returnErr error) {
	for _, host := range fetchNodeDetailsOptions.DatabaseOptions.RawHosts {
		fetchNodeDetailsOptions.DatabaseOptions.RawHosts = []string{host}
		_, err := vcc.VFetchNodesDetails(fetchNodeDetailsOptions)
		if err == nil {
			validHosts = append(validHosts, host)
		} else {
			invalidHosts = append(invalidHosts, host)
			returnErr = err
		}
	}
	return validHosts, invalidHosts, returnErr
}
