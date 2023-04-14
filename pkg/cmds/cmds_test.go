/*
 (c) Copyright [2021-2022] Open Text.
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
	"testing"

	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmds(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "cmds Suite")
}

var _ = Describe("k8s/cmds", func() {
	ctx := context.Background()

	It("should add the password option to vsql command", func() {
		cmd := []string{"-tAc", "select 1"}
		fpr := &FakePodRunner{SUPassword: "vertica"}
		podName := types.NamespacedName{Namespace: "default", Name: "vdb-pod"}
		_, _, _ = fpr.ExecVSQL(ctx, podName, "server", cmd...)
		lastCall := fpr.FindCommands("vsql", "--password", fpr.SUPassword, "-tAc", "select 1")
		Expect(len(lastCall)).Should(Equal(1))
	})

	It("should add password option for db_add_node", func() {
		cmd := []string{"-t", "db_add_node"}
		fpr := &FakePodRunner{SUPassword: "vertica"}
		podName := types.NamespacedName{Namespace: "default", Name: "vdb-pod"}
		_, _, _ = fpr.ExecAdmintools(ctx, podName, "server", cmd...)
		lastCall := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "db_add_node", "--password", fpr.SUPassword)
		Expect(len(lastCall)).Should(Equal(1))
	})

	It("should not add password to an admintools' tool which does not support it", func() {
		cmd := []string{"-t", "list_allnodes"}
		fpr := &FakePodRunner{SUPassword: "vertica"}
		podName := types.NamespacedName{Namespace: "default", Name: "vdb-pod"}
		_, _, _ = fpr.ExecAdmintools(ctx, podName, "server", cmd...)
		lastCall := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "list_allnodes")
		Expect(len(lastCall)).Should(Equal(1))
	})

	It("should not add password to vsql command", func() {
		cmd := []string{"-tAc", "select 1"}
		fpr := &FakePodRunner{SUPassword: ""}
		podName := types.NamespacedName{Namespace: "default", Name: "vdb-pod"}
		_, _, _ = fpr.ExecVSQL(ctx, podName, "server", cmd...)
		lastCall := fpr.FindCommands("vsql", "-tAc", "select 1")
		Expect(len(lastCall)).Should(Equal(1))
	})

	It("should not add password to admintools", func() {
		cmd := []string{"-t", "db_add_node"}
		fpr := &FakePodRunner{SUPassword: ""}
		podName := types.NamespacedName{Namespace: "default", Name: "vdb-pod"}
		_, _, _ = fpr.ExecAdmintools(ctx, podName, "server", cmd...)
		lastCall := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "db_add_node")
		Expect(len(lastCall)).Should(Equal(1))
	})
})
