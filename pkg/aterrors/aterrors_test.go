/*
 (c) Copyright [2021-2024] Open Text.
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

package aterrors

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("aterrors", func() {
	It("should log a diskfull event", func() {
		sampleOutput := `Info: no password specified, using none
*** Updating IP addresses for nodes of database vertdb ***
    Start update IP addresses for nodes
    Updating node IP addresses
    Generating new configuration information and reloading spread


Admintools was unable to complete a network operation while executing this request.
Sometimes this can be caused by transient network issues.
Please consider retrying the command.
Details:
--- Logging error ---\r
Traceback (most recent call last):\r
  File \"/opt/vertica/oss/python3/lib/python3.9/logging/__init__.py\", line 1087, in emit\r
    self.flush()\r
  File \"/opt/vertica/oss/python3/lib/python3.9/logging/__init__.py\", line 1067, in flush\r
    self.stream.flush()\r
OSError: [Errno 28] No space left on device\r
Call stack:\r
  File \"/opt/vertica/oss/python3/lib/python3.9/runpy.py\", line 197, in _run_module_as_main\r
    return _run_code(code, main_globals, None,\r
		`
		vdb := vapi.MakeVDB()
		tw := TestEVWriter{}
		evlogr := MakeATErrors(&tw, vdb, events.MgmtFailed)
		Expect(evlogr.LogFailure("restart_db", sampleOutput, nil)).Should(Equal(ctrl.Result{Requeue: true}))
		Expect(len(tw.RecordedEvents)).Should(Equal(1))
		Expect(tw.RecordedEvents[0].Reason).Should(Equal(events.MgmtFailedDiskFull))
	})

	It("should handle generic error that has no special output", func() {
		vdb := vapi.MakeVDB()
		tw := TestEVWriter{}
		const GenErrorReason = events.MgmtFailed
		evlogr := MakeATErrors(&tw, vdb, GenErrorReason)
		res, err := evlogr.LogFailure("test_cmd", "", nil)
		Expect(err).ShouldNot(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(len(tw.RecordedEvents)).Should(Equal(1))
		Expect(tw.RecordedEvents[0].Reason).Should(Equal(GenErrorReason))
	})

	It("should generate a requeue error for various known admintools errors", func() {
		vdb := vapi.MakeVDB()

		errStrings := []string{
			"Unable to connect to endpoint",
			"The specified bucket does not exist.",
			"Communal location [s3://blah] is not empty",
			"You are trying to access your S3 bucket using the wrong region. If you are using S3",
			"The authorization header is malformed; the region 'us-east-1' is wrong; expecting 'eu-central-1'.",
			"An error occurred during kerberos authentication",
		}

		for i := range errStrings {
			tw := TestEVWriter{}
			evlogr := MakeATErrors(&tw, vdb, events.CreateDBFailed)
			Expect(evlogr.LogFailure("create_db", errStrings[i], fmt.Errorf("error"))).Should(Equal(ctrl.Result{Requeue: true}))
		}
	})

	It("should requeue a short duration when admintools fails restart because some nodes are up", func() {
		vdb := vapi.MakeVDB()
		tw := TestEVWriter{}
		const GenErrorReason = events.MgmtFailed
		evlogr := MakeATErrors(&tw, vdb, GenErrorReason)
		Expect(evlogr.LogFailure("test_cmd", "All nodes in the input are not down, can't restart", nil)).Should(Equal(ctrl.Result{
			Requeue:      false,
			RequeueAfter: time.Second * RestartNodesNotDownRequeueWaitTimeInSeconds,
		}))
		Expect(len(tw.RecordedEvents)).Should(Equal(1))
		Expect(tw.RecordedEvents[0].Reason).Should(Equal(GenErrorReason))
	})

	It("should generate a requeue error for various known s3 errors", func() {
		vdb := vapi.MakeVDB()
		errStrings := []string{
			"Error: The database vertdb cannot continue because the communal storage location\n\ts3://nimbusdb/db\n" +
				"might still be in use.\n\nthe cluster lease will expire:\n\t2021-05-13 14:35:00.280925",
			"Could not copy file [s3://nimbusdb/db/empty/metadata/newdb/cluster_config.json] to [/tmp/desc.json]: " +
				"No such file or directory [s3://nimbusdb/db/empty/metadata/newdb/cluster_config.json]",
			"Could not copy file [gs://vertica-fleeting/mspilchen/revivedb-failures/metadata/vertdb/cluster_conf] to [/tmp/desc.json]: " +
				"File not found",
			"Could not copy file [webhdfs://vertdb/cluster_config.json] to [/tmp/desc.json]: Seen WebHDFS exception: " +
				"\nURL: [http://vertdb/cluster_config.json]\nHTTP response code: 404\nException type: FileNotFoundException",
			"Could not copy file [azb://cluster_config.json] to [/tmp/desc.json]: : The specified blob does not exist",
			"\n10.244.1.34 Permission Denied \n\n",
			"Database could not be revived.\nError: Node count mismatch",
			"Error: Primary node count mismatch:",
			"Could not copy file [s3://nimbusdb/db/spilly/metadata/vertdb/cluster_config.json] to [/tmp/desc.json]: Unable to connect to endpoint\n",
			"[/tmp/desc.json]: The specified bucket does not exist\nExit",
		}
		for i := range errStrings {
			tw := TestEVWriter{}
			evlogr := MakeATErrors(&tw, vdb, events.ReviveDBFailed)
			Expect(evlogr.LogFailure("revive_db", errStrings[i], fmt.Errorf("error"))).Should(Equal(ctrl.Result{Requeue: true}))
		}
	})

	It("should generate a requeue error for various comunal storage parameters", func() {
		vdb := vapi.MakeVDB()
		errStrings := []string{
			"Initialization thread logged exception:\\nInvalid configuration parameter wrongparm; aborting configuration change",
			"Initialization thread logged exception:\\nInvalid S3SseCustomerKey: key",
		}
		for i := range errStrings {
			tw := TestEVWriter{}
			evlogr := MakeATErrors(&tw, vdb, events.CreateDBFailed)
			Expect(evlogr.LogFailure("create_db", errStrings[i], fmt.Errorf("error"))).Should(Equal(ctrl.Result{Requeue: true}))
		}
	})
})
