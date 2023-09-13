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

package vadmin

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vcluster/rfc7807"
	"github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("verrors suite", func() {
	It("should handle case where non-rfc7807 is passed in", func() {
		vce := vcErrors{
			Log:      logger,
			EVWriter: &aterrors.TestEVWriter{},
		}
		origErr := errors.New("generic error")
		res, err := vce.LogFailure("test non-rfc7807", origErr)
		Ω(res).Should(Equal(ctrl.Result{}))
		Ω(err).Should(Equal(origErr))
	})

	It("should handle known rfc7807 error", func() {
		vce := vcErrors{
			Log:      logger,
			EVWriter: &aterrors.TestEVWriter{},
		}
		origErr := rfc7807.New(rfc7807.CommunalStorageNotEmpty).
			WithDetail("existing db already at /host").
			WithStatus(500).
			WithHost("pod-4")
		wrappedErr := errors.Join(origErr, errors.New("we hit an error"))
		res, err := vce.LogFailure("test rfc7807", wrappedErr)
		Ω(res).Should(Equal(ctrl.Result{Requeue: true}))
		Ω(err).Should(BeNil())
	})

	It("should handle unknown rfc7807 errors", func() {
		vce := vcErrors{
			Log:      logger,
			EVWriter: &aterrors.TestEVWriter{},
		}
		origErr := rfc7807.New(rfc7807.GenericBootstrapCatalogFailure).
			WithDetail("internal error occurred").
			WithStatus(500).
			WithHost("pod-5")
		res, err := vce.LogFailure("test unknown rfc7807", origErr)
		Ω(res).Should(Equal(ctrl.Result{}))
		Ω(err).Should(Equal(err))
	})

	It("should handle cluster lease expired errors", func() {
		vce := vcErrors{
			Log:      logger,
			EVWriter: &aterrors.TestEVWriter{},
			VDB:      vapi.MakeVDB(),
		}
		origErr := &vclusterops.ClusterLeaseNotExpiredError{Expiration: "10 minutes"}
		res, err := vce.LogFailure("test cluster lease error", origErr)
		Ω(res).Should(Equal(ctrl.Result{Requeue: true}))
		Ω(err).Should(BeNil())
	})

})
