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

package vdb

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
)

var _ = Describe("passwordsecret_reconcile", func() {
	const (
		suPassword1 = "su_password1"
		suPassword2 = "su_password2"
	)

	It("should return false if spec and status have different password secret", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.PasswordSecret = suPassword1
		vdb.Status.PasswordSecret = suPassword2
		a := PasswordSecretReconciler{Vdb: vdb, Log: logger}
		Expect(a.statusMatchesSpec()).To(BeFalse())
	})

	It("should return true if spec and status have the same password secret", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.PasswordSecret = suPassword1
		vdb.Status.PasswordSecret = suPassword1
		a := PasswordSecretReconciler{Vdb: vdb, Log: logger}
		Expect(a.statusMatchesSpec()).To(BeTrue())
	})
})
