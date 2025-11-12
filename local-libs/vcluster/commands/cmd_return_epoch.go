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
	"github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

/* CmdReturnEpoch
 *
 * Implements ClusterCommand interface
 */
type CmdReturnEpoch struct {
	returnEpochOptions *vclusterops.VReturnEpochOptions
	CmdBase
}

func makeCmdReturnEpoch() *cobra.Command {
	newCmd := &CmdReturnEpoch{}
	opt := vclusterops.VReturnEpochFactory()
	newCmd.returnEpochOptions = &opt

	cmd := makeBasicCobraCmd(
		newCmd,
		returnEpochSubCmd,
		"Returns the last good epoch.",
		`Returns the last good epoch.

Examples:
  # Return the last good epoch
  vcluster return_epoch
`,
		[]string{dbNameFlag, passwordFlag, hostsFlag, dbUserFlag, catalogPathFlag, configFlag},
	)

	return cmd
}

func (c *CmdReturnEpoch) Parse(inputArgv []string, logger vlog.Printer) error {
	c.argv = inputArgv
	logger.LogMaskedArgParse(c.argv)

	c.ResetUserInputOptions(&c.returnEpochOptions.DatabaseOptions)

	return c.validateParse(logger)
}

func (c *CmdReturnEpoch) validateParse(logger vlog.Printer) error {
	logger.Info("Called validateParse()")

	if !c.usePassword() {
		err := c.getCertFilesFromCertPaths(&c.returnEpochOptions.DatabaseOptions)
		if err != nil {
			return err
		}
	}

	err := c.ValidateParseBaseOptions(&c.returnEpochOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	err = c.setDBPassword(&c.returnEpochOptions.DatabaseOptions)
	if err != nil {
		return err
	}

	return nil
}

func (c *CmdReturnEpoch) Analyze(logger vlog.Printer) error {
	logger.Info("Called method Analyze()")
	return nil
}

func (c *CmdReturnEpoch) Run(vcc vclusterops.ClusterCommands) error {
	vcc.LogInfo("Called method Run()")

	options := c.returnEpochOptions

	epoch, err := vcc.VReturnEpoch(options)
	if err != nil {
		vcc.LogError(err, "failed to return epoch")
		return err
	}

	vcc.DisplayInfo("LastEpoch|%d", epoch)
	return nil
}

// SetDatabaseOptions will assign a vclusterops.DatabaseOptions instance to the one in CmdReturnEpoch
func (c *CmdReturnEpoch) SetDatabaseOptions(opt *vclusterops.DatabaseOptions) {
	c.returnEpochOptions.DatabaseOptions = *opt
}
