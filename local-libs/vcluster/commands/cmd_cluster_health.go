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
  vcluster cluster_health --start-time <start_time> --end-time <end_time>
`,
		[]string{dbNameFlag, configFlag, hostsFlag, ipv6Flag, passwordFlag, outputFileFlag},
	)

	// local flags
	newCmd.setLocalFlags(cmd)

	// Hide this command
	cmd.Hidden = true
	return cmd
}

// setLocalFlags will set the local flags the command has local flag of CmdClusterHealth
func (c *CmdClusterHealth) setLocalFlags(cmd *cobra.Command) {
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
	cmd.Flags().BoolVar(
		&c.clusterHealthOptions.Display,
		"display",
		false,
		"Wheather display the cascade graph in console",
	)
	cmd.Flags().StringVar(
		&c.clusterHealthOptions.Timezone,
		"timezone",
		"",
		"The timezone of the start and end time (e.g., -0500 or +0100). If not given, UTC will be used by default.",
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

	const (
		getSlowEvents    = "get_slow_events"
		getSessionStarts = "get_session_starts"
		getTxnStarts     = "get_transaction_starts"
		lockCascade      = "lock_cascade"
	)

	var bytes []byte
	switch c.clusterHealthOptions.Operation {
	case getSlowEvents:
		bytes, err = json.MarshalIndent(options.SlowEventsResult, "" /*prefix*/, " " /* indent for one space*/)
	case getSessionStarts:
		bytes, err = json.MarshalIndent(options.SessionStartsResult, "" /*prefix*/, " " /* indent for one space*/)
	case getTxnStarts:
		bytes, err = json.MarshalIndent(options.TransactionStartsResult, "" /*prefix*/, " " /* indent for one space*/)
	case lockCascade:
		bytes, err = json.MarshalIndent(options.LockEventCascade, "", " ")
	default: // by default, we will build a cascade graph
		bytes, err = json.MarshalIndent(options.SlowEventCascade, "", " ")
	}

	if err != nil {
		return fmt.Errorf("failed to marshal the traceback result, details: %w", err)
	}

	if options.Operation == "" {
		vcc.DisplayInfo("Successfully build the cascade graph for the slow events")
	} else if options.Operation == lockCascade {
		vcc.DisplayInfo("Successfully build the cascade graph for the lock events")
	}

	// output the result to console or file
	c.writeCmdOutputToFile(globals.file, bytes, vcc.GetLog())
	vcc.LogInfo("Slow event traceback: ", "slow events", string(bytes))

	if options.Display {
		if options.Operation == "" {
			options.DisplaySlowEventsCascade()
		} else if options.Operation == lockCascade {
			options.DisplayLockEventsCascade()
		}
	}

	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdClusterHealth
func (c *CmdClusterHealth) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.clusterHealthOptions.DatabaseOptions = *opt
}
