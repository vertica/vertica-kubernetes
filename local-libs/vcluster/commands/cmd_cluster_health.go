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
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdClusterHealth
 *
 * Implements ClusterCommand interface
 */
type CmdClusterHealth struct {
	clusterHealthOptions *vclusterops.VClusterHealthOptions

	CmdBase
}

func makeCmdClusterHealth() *cobra.Command {
	// CmdClusterHealth
	newCmd := &CmdClusterHealth{}
	opt := vclusterops.VClusterHealthFactory()
	newCmd.clusterHealthOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		clusterHealth,
		"Checks the database cluster health. This is used for testing and debugging only.",
		`Checks the database cluster health.
		
This is used for testing and debugging only.

Examples:
  # Check the cluster health
  vcluster cluster_health
`,
		// TODO: modify this
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, passwordFlag, outputFileFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	return cmd
}

// setLocalFlags will set the local flags the command has
func (c *CmdClusterHealth) setLocalFlags(cmd *cobra.Command) {
	// TODO: add some code here
	// local flag of CmdClusterHealth,
	// --operation : the operation type, including "check", "get_slow_events", "get_transaction_starts", "get_session_starts"
	// --txn-id : the transaction id (for slow event and trsanction start)
	// --node-name : the node name (for all operations)
	// --start-time : the start time (for all operations)
	// --end-time : the end time (for all operations)
	// --session-id : the session id  (for session start and slow event)
	// --debug : debug mode  (for all operations)
	// --threadhold : the threadhold of seconds for slow events (for get_slow_events)
	// --thread-id : the thread id (for get_slow_events)
	// --phase-duration-desc : the phase duration description (for get_slow_events)
	// --event-desc : the event description (for get_slow_events)
	// --user-name : the user name (for get_slow_events)

	cmd.Flags().StringVar(
		&c.clusterHealthOptions.Operation,
		"operation",
		"",
		"The operation type, including 'cascade', 'get_slow_events', 'get_transaction_starts', 'get_session_starts'.",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.TxnID,
		"txn-id",
		"",
		"The transaction id (for slow event and transaction start).",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.NodeName,
		"node-name",
		"",
		"The node name (for all operations).",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.StartTime,
		"start-time",
		"",
		"The start time (for all operations).",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.EndTime,
		"end-time",
		"",
		"The end time (for all operations).",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.SessionID,
		"session-id",
		"",
		"The session id (for session start and slow event).",
	)
	cmd.Flags().BoolVar(
		&c.clusterHealthOptions.Debug,
		"debug",
		false,
		"Debug mode (for all operations).",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.Threadhold,
		"threadhold",
		"",
		"The threadhold of seconds for slow events (for get_slow_events).",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.ThreadID,
		"thread-id",
		"",
		"The thread id (for get_slow_events).",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.PhaseDurationDesc,
		"phase-duration-desc",
		"",
		"The phase duration description (for get_slow_events).",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.EventDesc,
		"event-desc",
		"",
		"The event description (for get_slow_events).",
	)
}

func (c *CmdClusterHealth) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	// for some options, we do not want to use their default values,
	// if they are not provided in cli,
	// reset the value of those options to nil
	c.ResetUserInputOptions(&c.clusterHealthOptions.DatabaseOptions)
	return c.validateParse(logger)
}

func (c *CmdClusterHealth) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.clusterHealthOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	// validate txn id is integer
	if c.clusterHealthOptions.TxnID != "" {
		_, err := c.validateInt(c.clusterHealthOptions.TxnID, "txn-id")
		if err != nil {
			return err
		}
	}
	err := c.ValidateParseBaseOptions(&c.clusterHealthOptions.DatabaseOptions)
	if err != nil {
		return err
	}
	return c.setDBPassword(&c.clusterHealthOptions.DatabaseOptions)
}

// validateInt checks if the given string is a valid integer
func (c *CmdClusterHealth) validateInt(value, fieldName string) (int, error) {
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", fieldName)
	}
	return intValue, nil
}

func (c *CmdClusterHealth) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.clusterHealthOptions

	err := vcc.VClusterHealth(options)
	if err != nil {
		vcc.LogError(err, "failed to check cluster health.")
		return err
	}

	bytes, err := json.MarshalIndent(options.CascadeStack, "", " ")
	if err != nil {
		return fmt.Errorf("failed to marshal the traceback result, details: %w", err)
	}

	c.writeCmdOutputToFile(globals.file, bytes, vcc.GetLog())
	vcc.LogInfo("Slow event traceback: ", "slow events", string(bytes))

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdClusterHealth
func (c *CmdClusterHealth) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.clusterHealthOptions.DatabaseOptions = *opt
}
