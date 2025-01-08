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
	"io"
	"os"

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
