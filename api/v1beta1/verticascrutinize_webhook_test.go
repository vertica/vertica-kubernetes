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

package v1beta1

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
)

var _ = Describe("verticascrutinize_webhook", func() {
	It("should succeed with no log age times", func() {
		vscr := MakeVscr()
		Expect(vscr.ValidateCreate()).Should(Succeed())
		Expect(vscr.ValidateUpdate(vscr)).Should(Succeed())
	})

	It("should succeed with log-age-hours only", func() {
		vscr := MakeVscr()
		vscr.Annotations[vmeta.ScrutinizeLogAgeHours] = "8"
		Expect(vscr.ValidateCreate()).Should(Succeed())
		Expect(vscr.ValidateUpdate(vscr)).Should(Succeed())
	})

	It("should succeed with valid log-age-oldest-time and log-age-newest-time", func() {
		vscr := MakeVscr()
		vscr.Annotations[vmeta.ScrutinizeLogAgeOldestTime] = GenerateLogAgeTime(-8, "-05")
		vscr.Annotations[vmeta.ScrutinizeLogAgeNewestTime] = GenerateLogAgeTime(24, "")
		Expect(vscr.ValidateCreate()).Should(Succeed())
		Expect(vscr.ValidateUpdate(vscr)).Should(Succeed())
	})

	It("should fail if set log-age-hours together with log-age-oldest-time or log-age-newest-time", func() {
		vscr := MakeVscr()
		vscr.Annotations[vmeta.ScrutinizeLogAgeHours] = "8"
		vscr.Annotations[vmeta.ScrutinizeLogAgeOldestTime] = GenerateLogAgeTime(-8, "-05")
		vscr.Annotations[vmeta.ScrutinizeLogAgeNewestTime] = GenerateLogAgeTime(24, "")
		err := vscr.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("log-age-hours cannot be set alongside log-age-oldest-time and log-age-newest-time"))
	})

	It("should fail if log-age-hours is negative", func() {
		vscr := MakeVscr()
		vscr.Annotations[vmeta.ScrutinizeLogAgeHours] = "-8"
		err := vscr.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("log-age-hours cannot be negative"))
	})

	It("should fail if log-age-oldest-time is after current time", func() {
		vscr := MakeVscr()
		vscr.Annotations[vmeta.ScrutinizeLogAgeOldestTime] = GenerateLogAgeTime(22, "+08")
		err := vscr.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("log-age-oldest-time cannot be set after current time"))
	})

	It("should fail if log-age-newest-time is before LogAgeOldestTime", func() {
		vscr := MakeVscr()
		vscr.Annotations[vmeta.ScrutinizeLogAgeOldestTime] = GenerateLogAgeTime(-4, "-05")
		vscr.Annotations[vmeta.ScrutinizeLogAgeNewestTime] = GenerateLogAgeTime(-24, "-05")
		err := vscr.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("log-age-oldest-time cannot be set after log-age-newest-time"))
	})

	// generate a time in RFC1123 format, for example: "Mon, 02 Jan 2006 15:04:05 MST"
	It("should fail if log-age-oldest-time is in wrong format", func() {
		vscr := MakeVscr()
		vscr.Annotations[vmeta.ScrutinizeLogAgeOldestTime] = time.Now().AddDate(0, 0, -1).Format(time.RFC1123)
		err := vscr.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("should be formatted as: YYYY-MM-DD HH [+/-XX]"))
	})

	// test with regular string
	It("should fail if log-age-newest-time is in wrong format", func() {
		vscr := MakeVscr()
		vscr.Annotations[vmeta.ScrutinizeLogAgeNewestTime] = "invalid time format"
		err := vscr.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("should be formatted as: YYYY-MM-DD HH [+/-XX]"))
	})

})
