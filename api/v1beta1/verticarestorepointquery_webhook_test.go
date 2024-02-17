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

package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("verticarestorepointsquery_webhook", func() {
	It("should succeed with no filter option fields", func() {
		vrpq := MakeVrpq()
		Expect(vrpq.ValidateCreate()).Should(Succeed())
		Expect(vrpq.ValidateUpdate(vrpq)).Should(Succeed())
	})

	It("should succeed with all valid fields", func() {
		vrpq := MakeVrpq()
		vrpq.Spec.FilterOptions.ArchiveName = "db"
		vrpq.Spec.FilterOptions.StartTimestamp = "2006-01-02 23:59:56"
		vrpq.Spec.FilterOptions.EndTimestamp = "2006-01-02 23:59:58"
		Expect(vrpq.ValidateCreate()).Should(Succeed())
		Expect(vrpq.ValidateUpdate(vrpq)).Should(Succeed())
	})

	It("should fail if invalid start timestamp", func() {
		vrpq := MakeVrpq()
		vrpq.Spec.FilterOptions.StartTimestamp = "start"
		err := vrpq.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("start timestamp \"start\" is invalid; cannot parse as a datetime"))
	})

	It("should fail if invalid end timestamp", func() {
		vrpq := MakeVrpq()
		vrpq.Spec.FilterOptions.EndTimestamp = "end"
		err := vrpq.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("end timestamp \"end\" is invalid; cannot parse as a datetime"))
	})

	It("should fail if invalid endTimestamp is before startTimestamp ", func() {
		vrpq := MakeVrpq()
		vrpq.Spec.FilterOptions.StartTimestamp = "2006-01-02 23:59:59.123456789"
		vrpq.Spec.FilterOptions.EndTimestamp = "2006-01-02 23:59:59"
		err := vrpq.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("start timestamp must be before end timestamp"))
	})
})
