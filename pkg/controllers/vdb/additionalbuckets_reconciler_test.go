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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
)

var _ = Describe("additioanalbuckets_reconcile", func() {
	ctx := context.Background()

	It("should return false if status is nil", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.AdditionalBuckets = []vapi.CommunalStorage{
			{Path: "s3://bucket1", Region: "us-east-1", Endpoint: "https://s3.example.com", CredentialSecret: "secret1"},
		}
		a := AddtionalBucketsReconciler{Vdb: vdb, Log: logger}
		Expect(a.statusMatchesSpec()).To(BeFalse())
	})

	It("should return false if spec and status have different lengths", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.AdditionalBuckets = []vapi.CommunalStorage{
			{Path: "s3://bucket1", Region: "us-east-1", Endpoint: "https://s3.example.com", CredentialSecret: "secret1"},
			{Path: "gs://bucket2", Region: "us-east-1", Endpoint: "https://gs.example.com", CredentialSecret: "secret2"},
		}
		vdb.Status.AdditionalBuckets = []vapi.CommunalStorage{
			{Path: "s3://bucket1", Region: "us-east-1", Endpoint: "https://s3.example.com", CredentialSecret: "secret1"},
		}
		a := AddtionalBucketsReconciler{Vdb: vdb, Log: logger}
		Expect(a.statusMatchesSpec()).To(BeFalse())
	})

	It("should return false if any field differs", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.AdditionalBuckets = []vapi.CommunalStorage{
			{Path: "s3://bucket1", Region: "us-east-1", Endpoint: "https://s3.example.com", CredentialSecret: "secret1"},
		}
		vdb.Status.AdditionalBuckets = []vapi.CommunalStorage{
			{Path: "s3://bucket2", Region: "us-east-1", Endpoint: "https://s3.example.com", CredentialSecret: "secret1"},
		}
		a := AddtionalBucketsReconciler{Vdb: vdb, Log: logger}
		Expect(a.statusMatchesSpec()).To(BeFalse())

		vdb.Status.AdditionalBuckets[0].Path = "s3://bucket1"
		vdb.Status.AdditionalBuckets[0].Region = "us-west-2"
		Expect(a.statusMatchesSpec()).To(BeFalse())

		vdb.Status.AdditionalBuckets[0].Region = "us-east-1"
		vdb.Status.AdditionalBuckets[0].Endpoint = "https://other.example.com"
		Expect(a.statusMatchesSpec()).To(BeFalse())

		vdb.Status.AdditionalBuckets[0].Endpoint = "https://s3.example.com"
		vdb.Status.AdditionalBuckets[0].CredentialSecret = "secret2"
		Expect(a.statusMatchesSpec()).To(BeFalse())
	})

	It("should return true if all fields match", func() {
		vdb := vapi.MakeVDB()
		bucket := vapi.CommunalStorage{
			Path: "s3://bucket1", Region: "us-east-1", Endpoint: "https://s3.example.com", CredentialSecret: "secret1",
		}
		vdb.Spec.AdditionalBuckets = []vapi.CommunalStorage{bucket}
		vdb.Status.AdditionalBuckets = []vapi.CommunalStorage{bucket}
		a := AddtionalBucketsReconciler{Vdb: vdb, Log: logger}
		Expect(a.statusMatchesSpec()).To(BeTrue())
	})

	It("should update AdditionalBuckets in status to match spec", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.AdditionalBuckets = []vapi.CommunalStorage{
			{
				Path:             "s3://mybucket/extra",
				Endpoint:         "https://s3.example.com",
				Region:           "us-east-1",
				CredentialSecret: "secret1",
			},
			{
				Path:             "gs://anotherbucket",
				Endpoint:         "https://gs.example.com",
				Region:           "us-central1",
				CredentialSecret: "secret2",
			},
			{
				Path:             "azb://azbaccount/anotherbucket",
				Endpoint:         "https://azbaccount.blob.core.windows.net",
				Region:           "us-central1",
				CredentialSecret: "secret3",
			},
		}
		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.PodFacts{}
		rec := MakeAddtionalBucketsReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		r := rec.(*AddtionalBucketsReconciler)

		// Add this line to create the vdb in the fake client
		test.CreateVDB(ctx, r.Client, vdb)
		defer test.DeleteVDB(ctx, r.Client, vdb)

		// Status should be empty before update
		Expect(vdb.Status.AdditionalBuckets).To(BeNil())
		Expect(r.statusMatchesSpec()).To(BeFalse())

		err := r.updateAdditionalBucketsStatus(ctx)
		Expect(err).Should(BeNil())
		Expect(vdb.Status.AdditionalBuckets).To(Equal(vdb.Spec.AdditionalBuckets))
	})
})
