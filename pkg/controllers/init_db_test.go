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

package controllers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
)

var _ = Describe("s3_auth", func() {
	ctx := context.Background()

	It("should be able to read the auth from secret", func() {
		vdb := vapi.MakeVDB()
		createCommunalCredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			VRec:    vrec,
			Log:     logger,
			Vdb:     vdb,
			PRunner: fpr,
		}
		Expect(g.getS3Auth(ctx)).Should(Equal(fmt.Sprintf("%s:%s", testAccessKey, testSecretKey)))
	})

	It("should return s3 endpoint stripped of https/http", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Endpoint = "https://192.168.0.1"

		fpr := &cmds.FakePodRunner{}
		g := GenericDatabaseInitializer{
			VRec:    vrec,
			Log:     logger,
			Vdb:     vdb,
			PRunner: fpr,
		}

		Expect(g.getS3Endpoint()).Should(Equal("192.168.0.1"))
		Expect(g.getEnableHTTPS()).Should(Equal("1"))

		vdb.Spec.Communal.Endpoint = "http://fqdn.example.com:8080"

		Expect(g.getS3Endpoint()).Should(Equal("fqdn.example.com:8080"))
		Expect(g.getEnableHTTPS()).Should(Equal("0"))
	})

	It("should fail to get host list if some pods not running", func() {
		vdb := vapi.MakeVDB()
		const ScIndex = 0
		const ScSize = 2
		vdb.Spec.Subclusters[ScIndex].Size = ScSize
		createPods(ctx, vdb, AllPodsNotRunning)
		defer deletePods(ctx, vdb)
		const PodIndex = 0
		setPodStatus(ctx, 1 /* funcOffset */, names.GenPodName(vdb, &vdb.Spec.Subclusters[ScIndex], PodIndex), ScIndex, PodIndex, AllPodsRunning)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, ScSize)

		g := GenericDatabaseInitializer{
			VRec:    vrec,
			Log:     logger,
			Vdb:     vdb,
			PRunner: fpr,
			PFacts:  pfacts,
		}
		podList := []*PodFact{}
		for i := range pfacts.Detail {
			podList = append(podList, pfacts.Detail[i])
		}
		ok := g.checkPodList(podList)
		Expect(ok).Should(BeFalse())
	})
})
