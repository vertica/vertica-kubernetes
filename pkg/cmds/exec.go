/*
 (c) Copyright [2021-2023] Open Text.
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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type PodRunner interface {
	ExecInPod(ctx context.Context, podName types.NamespacedName, contName string, command ...string) (string, string, error)
	ExecVSQL(ctx context.Context, podName types.NamespacedName, contName string, command ...string) (string, string, error)
	ExecAdmintools(ctx context.Context, podName types.NamespacedName, contName string, command ...string) (string, string, error)
	CopyToPod(ctx context.Context, podName types.NamespacedName, contName string, sourceFile string,
		destFile string, executeCmd ...string) (stdout, stderr string, err error)
	DumpAdmintoolsConf(ctx context.Context, podName types.NamespacedName)
}

type ClusterPodRunner struct {
	Log        logr.Logger
	Cfg        *rest.Config
	SUPassword string
}

// MakeClusterPodRunnerr will build a ClusterPodRunner object
func MakeClusterPodRunner(log logr.Logger, cfg *rest.Config, passwd string) *ClusterPodRunner {
	return &ClusterPodRunner{Log: log, Cfg: cfg, SUPassword: passwd}
}

// logInfoCmd calls log function for the given command
func (c *ClusterPodRunner) logInfoCmd(podName types.NamespacedName, command ...string) {
	c.Log.Info("ExecInPod entry", "pod", podName, "command", generateLogOutput(command...))
}

// generateLogOutput will produce the output suitable for logging purposes. It
// will obfuscate any sensitive information like passwords or credentials.
func generateLogOutput(cmd ...string) string {
	var sb strings.Builder
	for i := 0; i < len(cmd); i++ {
		switch cmd[i] {
		case "--password":
			sb.WriteString(cmd[i])
			sb.WriteString(" ")
			sb.WriteString("*******")
			i++
		default:
			sb.WriteString(obfuscateForLog(cmd[i]))
		}
		sb.WriteString(" ")
	}
	return sb.String()
}

// obfuscateForLog is a helper function to obfuscate any sensitive info in a
// command. It returns the obfuscated string.
func obfuscateForLog(s string) string {
	pats := map[string]string{
		"awsauth = .*":                 "awsauth = ****",
		"GCSAuth = .*":                 "GCSAuth = ****",
		"AzureStorageCredentials = .*": "AzureStorageCredentials = ****",
	}
	for expr, replacement := range pats {
		r := regexp.MustCompile(expr)
		s = r.ReplaceAllString(s, replacement)
	}
	return s
}

// ExecInPod executes arbitrary command inside of a pod and returns the output.
func (c *ClusterPodRunner) ExecInPod(ctx context.Context, podName types.NamespacedName,
	contName string, command ...string) (stdout, stderr string, err error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	err = c.postExec(ctx, podName, contName, command, &execOut, &execErr, nil)
	return execOut.String(), execErr.String(), err
}

// CopyToPod copies a file into a container's pod. Optionally, it can also run a
// command after the copy has finished.
func (c *ClusterPodRunner) CopyToPod(ctx context.Context, podName types.NamespacedName,
	contName string, sourceFile string, destFile string, executeCmd ...string) (stdout, stderr string, err error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	// Copying a file is simply a cat of the contents from stdin
	var sb strings.Builder
	sb.WriteString("cat > ")
	sb.WriteString(destFile)
	// If an execute command was given, we tack this on the end as something
	// that will run after the file has been copied in.
	if executeCmd != nil {
		sb.WriteString(" && ")
		sb.WriteString(strings.Join(executeCmd, " "))
	}
	command := []string{"sh", "-c", sb.String()}

	inFile, err := os.Open(sourceFile)
	if err != nil {
		return "", "", err
	}
	defer inFile.Close()

	err = c.postExec(ctx, podName, contName, command, &execOut, &execErr, inFile)
	return execOut.String(), execErr.String(), err
}

// ExecVSQL appends options to the vsql command and calls ExecInPod
func (c *ClusterPodRunner) ExecVSQL(ctx context.Context, podName types.NamespacedName,
	contName string, command ...string) (stdout, stderr string, err error) {
	command = UpdateVsqlCmd(c.SUPassword, command...)
	return c.ExecInPod(ctx, podName, contName, command...)
}

// ExecAdmintools appends options to the admintools command and calls ExecInPod
func (c *ClusterPodRunner) ExecAdmintools(ctx context.Context, podName types.NamespacedName,
	contName string, command ...string) (stdout, stderr string, err error) {
	command = UpdateAdmintoolsCmd(c.SUPassword, command...)
	return c.ExecInPod(ctx, podName, contName, command...)
}

// DumpAdmintoolsConf will log relenvant portions of the admintools.conf for debug purposes.
func (c *ClusterPodRunner) DumpAdmintoolsConf(ctx context.Context, podName types.NamespacedName) {
	// Dump out vital informating from admintools.conf for logging purposes. We
	// rely on the logging that is done inside ExecInPod.
	cmd := []string{
		"bash", "-c",
		fmt.Sprintf(`ls -l %s && grep '^node\|^v_\|^host' %s`, paths.AdminToolsConf, paths.AdminToolsConf),
	}
	// Since this is for debugging purposes all errors are ignored
	c.ExecInPod(ctx, podName, names.ServerContainer, cmd...) //nolint:errcheck
}

// UpdateVsqlCmd generates a vsql command appending the options we need
func UpdateVsqlCmd(passwd string, cmd ...string) []string {
	prefix := []string{"vsql"}
	if passwd != "" {
		prefix = []string{"vsql", "--password", passwd}
	}
	cmd = append(prefix, cmd...)
	return cmd
}

// UpdateAdmintoolsCmd generates an admintools command appending the options we need
func UpdateAdmintoolsCmd(passwd string, cmd ...string) []string {
	// We are running as dbadmin, but we need to do this 'sudo su dbadmin --'
	// stuff so that we have the proper ulimits set.  When you exec into a pod,
	// the ulimits you use are for the container runtime.  This can differ from
	// the actual limits for the pod/container.  So we need this extra bit to
	// ensure we always run with the pod limits.  This ensures the limits are
	// the same across all vertica nodes.
	//
	// The --preserve-env option is required so that environment variables flow
	// through to the vertica process.
	prefix := []string{"sudo", "--preserve-env", "su", "dbadmin", "--", "/opt/vertica/bin/admintools"}
	cmd = append(prefix, cmd...)
	if passwd == "" {
		return cmd
	}
	supportingPasswdSlice := getSupportingPasswdSlice()
	for _, e := range supportingPasswdSlice {
		_, isPresent := Find(cmd, e)
		if isPresent {
			cmd = append(cmd, "--password", passwd)
			break
		}
	}
	return cmd
}

// Find checks if a slice contains a string and at which position
func Find(slice []string, option string) (int, bool) {
	for i, item := range slice {
		if item == option {
			return i, true
		}
	}
	return -1, false
}

// GetSupportingPasswdSlice returns a list of admintools' tools
// used inside the operator and for which the option --password pswd is supported
func getSupportingPasswdSlice() []string {
	return []string{
		"db_add_node", "db_add_subcluster", "db_remove_node",
		"db_remove_subcluster", "create_db", "restart_node", "start_db",
		"stop_db",
	}
}

// postExec makes the actual POST call to the REST endpoint to do the exec
func (c *ClusterPodRunner) postExec(ctx context.Context, podName types.NamespacedName, contName string, command []string,
	execOut, execErr *bytes.Buffer, execIn io.Reader) error {
	c.logInfoCmd(podName, command...)

	cli, err := kubernetes.NewForConfig(c.Cfg)
	if err != nil {
		return fmt.Errorf("could not get clientset: %v", err)
	}

	hasStdin := false
	if execIn != nil {
		hasStdin = true
	}
	restClient := cli.CoreV1().RESTClient()
	req := restClient.Post().
		Resource("pods").
		Name(podName.Name).
		Namespace(podName.Namespace).
		SubResource("exec")
	req.VersionedParams(&corev1.PodExecOptions{
		Container: contName,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
		Stdin:     hasStdin,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.Cfg, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to init executor: %v", err)
	}

	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: execOut,
		Stderr: execErr,
		Stdin:  execIn,
	})
	c.Log.Info("ExecInPod stream", "pod", podName, "err", err, "stdout", execOut.String(), "stderr", execErr.String())

	if err != nil {
		return fmt.Errorf("could not execute: %v", err)
	}

	return nil
}
