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

package commands

import (
	"github.com/spf13/cobra"
)

/* CmdUpgradeVertica
 *
 * A subcommand managing the YAML config file
 * in the default or a specified directory.
 *
 * Implements ClusterCommand interface
 */

func makeCmdUpgradeVertica() *cobra.Command {
	cmd := makeSimpleCobraCmd(
		upgradeVerticaSubCmd,
		"Performs one of the three steps of upgrading a vertica cluster: create_sandbox, promote_sandbox, or finalize.",
		`Performs one of the three steps of upgrading a vertica cluster: create_sandbox, promote_sandbox, or finalize.`)

	cmd.AddCommand(makeCmdUpgradePart1())
	cmd.AddCommand(makeCmdUpgradePart2())
	cmd.AddCommand(makeCmdUpgradePart3())

	return cmd
}
