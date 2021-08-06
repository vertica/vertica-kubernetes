/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package cmds

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

// FakePodRunner is stub that we use in testing to take output from exec calls.
// The CmdResult are prepopulated results to commands.  Each command given is for
// a specific pod. This allows us to build up the result for each successive
// exec call to that pod. This class also keeps track of the commands that were
// passed to ExecInPod. These can be inspected at the end of the test to verify
// assertions.
type FakePodRunner struct {
	// The fake result of calls made.  This *must* be filled in prior to ExecInPod.
	Results CmdResults
	// The commands that were issue. The commands are in the same order that
	// commands were received. This is filled in by ExecInPod and can be inspected.
	Histories []CmdHistory
	// fake password
	SUPassword string
}

// CmdResults stores the command result.  The key is the pod name.
type CmdResults map[types.NamespacedName][]CmdResult

// CmdResult stores the result of a single command.
type CmdResult struct {
	Stdout string
	Stderr string
	Err    error
}

// CmdHistory stores the command that was run and the pod it was run against.
type CmdHistory struct {
	Pod     types.NamespacedName
	Command []string
}

// ExecInPod is a test stub for a real exec call to a pod.
// It will return output as saved in the FakePodRunner struct. The command that
// is passed in are saved as a history that tests can later inspect.
func (f *FakePodRunner) ExecInPod(ctx context.Context, podName types.NamespacedName,
	contName string, command ...string) (stdout, stderr string, err error) {
	// Record the call that come in.  Some testcases can use this in assertions.
	f.Histories = append(f.Histories, CmdHistory{Pod: podName, Command: command})
	// We fake out what is returned by doing a lookup in fakePodOutputs
	res, ok := f.Results[podName]
	if !ok || len(res) == 0 {
		return "", "", nil
	}
	execReturn := res[0]
	res = res[1:]
	f.Results[podName] = res
	return execReturn.Stdout, execReturn.Stderr, execReturn.Err
}

// ExecAdmintools calls ExecInPod
func (f *FakePodRunner) ExecAdmintools(ctx context.Context, podName types.NamespacedName,
	contName string, command ...string) (stdout, stderr string, err error) {
	command = UpdateAdmintoolsCmd(f.SUPassword, command...)
	return f.ExecInPod(ctx, podName, contName, command...)
}

// ExecVSQL calls ExecInPod
func (f *FakePodRunner) ExecVSQL(ctx context.Context, podName types.NamespacedName,
	contName string, command ...string) (stdout, stderr string, err error) {
	command = UpdateVsqlCmd(f.SUPassword, command...)
	return f.ExecInPod(ctx, podName, contName, command...)
}

// CopyToPod will mimic a real copy file into a pod
func (f *FakePodRunner) CopyToPod(ctx context.Context, podName types.NamespacedName,
	contName string, sourceFile string, destFile string) (stdout, stderr string, err error) {
	command := []string{"sh", "-c", fmt.Sprintf("cat > %s", destFile)}
	return f.ExecInPod(ctx, podName, contName, command...)
}

// FindCommands will search through the command history for any command that
// contains the given partial command.
func (f *FakePodRunner) FindCommands(partialCmd ...string) []CmdHistory {
	partialCmdStr := strings.Join(partialCmd, " ")
	cmds := []CmdHistory{}
	for _, c := range f.Histories {
		// Build a single string with the entire command.
		fullCmd := strings.Join(c.Command, " ")
		if strings.Contains(fullCmd, partialCmdStr) {
			cmds = append(cmds, c)
		}
	}
	return cmds
}
