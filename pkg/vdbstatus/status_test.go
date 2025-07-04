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

package vdbstatus

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var k8sClient client.Client
var testEnv *envtest.Environment
var logger logr.Logger

var _ = BeforeSuite(func() {
	logger = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	logf.SetLogger(logger)

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, cfg).NotTo(BeNil())
	restCfg := cfg

	err = vapi.AddToScheme(scheme.Scheme)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "vdbstatus Suite")
}

var _ = Describe("status", func() {
	ctx := context.Background()
	const secretName = "test"

	It("should update status condition when no conditions have been set", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		cond := vapi.MakeCondition(vapi.AutoRestartVertica, metav1.ConditionTrue, "")
		Expect(UpdateCondition(ctx, k8sClient, vdb, cond)).Should(Succeed())
		fetchVdb := &vapi.VerticaDB{}
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
			Expect(len(v.Status.Conditions)).Should(Equal(1))
			Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(*cond))
		}
	})

	It("should be able to change an existing status condition", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		conds := []metav1.Condition{
			{Type: vapi.AutoRestartVertica, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
			{Type: vapi.AutoRestartVertica, Status: metav1.ConditionFalse, Reason: vapi.UnknownReason},
		}

		for i := range conds {
			Expect(UpdateCondition(ctx, k8sClient, vdb, &conds[i])).Should(Succeed())
			fetchVdb := &vapi.VerticaDB{}
			nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
			Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
			for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
				Expect(len(v.Status.Conditions)).Should(Equal(1))
				Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(conds[i]))
			}
		}
	})

	It("should update sandbox upgrade state", func() {
		vdb := vapi.MakeVDB()
		const sbName = "sb1"
		const scName = "sc1"
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{scName}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		state := vdb.Status.Sandboxes[0].UpgradeState.DeepCopy()
		state.UpgradeInProgress = true
		const msg = "test"
		state.UpgradeStatus = msg
		Expect(SetSandboxUpgradeState(ctx, k8sClient, vdb, sbName, state)).Should(Succeed())
		fetchVdb := &vapi.VerticaDB{}
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
			Expect(v.Status.Sandboxes[0].UpgradeState.UpgradeInProgress).Should(BeTrue())
			Expect(v.Status.Sandboxes[0].UpgradeState.UpgradeStatus).Should(Equal(msg))
		}
	})

	It("should be able to handle multiple status conditions", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		conds := []metav1.Condition{
			{Type: vapi.DBInitialized, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
			{Type: vapi.AutoRestartVertica, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
			{Type: vapi.AutoRestartVertica, Status: metav1.ConditionFalse, Reason: vapi.UnknownReason},
		}

		for i := range conds {
			Expect(UpdateCondition(ctx, k8sClient, vdb, &conds[i])).Should(Succeed())
		}

		fetchVdb := &vapi.VerticaDB{}
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
			Expect(len(v.Status.Conditions)).Should(Equal(2))
			Expect(v.Status.Conditions[0]).Should(test.EqualMetaV1Condition(conds[0]))
			Expect(v.Status.Conditions[1]).Should(test.EqualMetaV1Condition(conds[2]))
		}
	})

	It("should change the lastTransitionTime when a status condition is changed", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		origTime := metav1.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
		Expect(UpdateCondition(ctx, k8sClient, vdb,
			&metav1.Condition{Type: vapi.AutoRestartVertica, Status: metav1.ConditionFalse, LastTransitionTime: origTime,
				Reason: vapi.UnknownReason},
		)).Should(Succeed())
		Expect(UpdateCondition(ctx, k8sClient, vdb,
			&metav1.Condition{Type: vapi.AutoRestartVertica, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
		)).Should(Succeed())
		Expect(vdb.Status.Conditions[0].LastTransitionTime).ShouldNot(Equal(origTime))
	})

	It("should return false in IsStatusConditionTrue if condition isn't present", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		Expect(vdb.IsStatusConditionTrue(vapi.VerticaRestartNeeded)).Should(BeFalse())
		Expect(UpdateCondition(ctx, k8sClient, vdb,
			&metav1.Condition{Type: vapi.VerticaRestartNeeded, Status: metav1.ConditionTrue, Reason: vapi.UnknownReason},
		)).Should(Succeed())
		Expect(vdb.IsStatusConditionTrue(vapi.VerticaRestartNeeded)).Should(BeTrue())
	})

	It("should set/clear the upgrade status message", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		Expect(SetUpgradeStatusMessage(ctx, k8sClient, vdb, "upgrade started")).Should(Succeed())

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.UpgradeStatus).Should(Equal("upgrade started"))
	})

	It("should update status secret when no secrets have been set", func() {
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		config := &vapi.TLSConfigStatus{
			Name:   vapi.HTTPSNMATLSConfigName,
			Secret: secretName,
		}
		Expect(UpdateTLSConfigs(ctx, k8sClient, vdb, []*vapi.TLSConfigStatus{config})).Should(Succeed())
		fetchVdb := &vapi.VerticaDB{}
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
			Expect(len(v.Status.TLSConfigs)).Should(Equal(1))
			Expect(v.Status.TLSConfigs[0].Name).Should(Equal(vapi.HTTPSNMATLSConfigName))
			Expect(v.Status.TLSConfigs[0].Secret).Should(Equal(secretName))
		}
	})

	It("should be able to change an existing status secret", func() {
		const sn = "test1"
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		configs := []vapi.TLSConfigStatus{
			{Name: vapi.HTTPSNMATLSConfigName, Secret: sn},
			{Name: vapi.HTTPSNMATLSConfigName, Secret: secretName},
		}

		for i := range configs {
			Expect(UpdateTLSConfigs(ctx, k8sClient, vdb, []*vapi.TLSConfigStatus{&configs[i]})).Should(Succeed())
			fetchVdb := &vapi.VerticaDB{}
			nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
			Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
			for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
				Expect(len(v.Status.TLSConfigs)).Should(Equal(1))
				Expect(v.Status.TLSConfigs[0].Name).Should(Equal(configs[i].Name))
				Expect(v.Status.TLSConfigs[0].Secret).Should(Equal(configs[i].Secret))
			}
		}
	})

	It("should be able to handle multiple status secrets", func() {
		const sn = "test1"
		vdb := vapi.MakeVDB()
		Expect(k8sClient.Create(ctx, vdb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vdb)).Should(Succeed()) }()

		configs := []vapi.TLSConfigStatus{
			{Name: "type1", Secret: "sec1"},
			{Name: vapi.HTTPSNMATLSConfigName, Secret: sn},
			{Name: vapi.HTTPSNMATLSConfigName, Secret: secretName},
		}

		for i := range configs {
			Expect(UpdateTLSConfigs(ctx, k8sClient, vdb, []*vapi.TLSConfigStatus{&configs[i]})).Should(Succeed())
		}

		fetchVdb := &vapi.VerticaDB{}
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Name}
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		for _, v := range []*vapi.VerticaDB{vdb, fetchVdb} {
			Expect(len(v.Status.TLSConfigs)).Should(Equal(2))
			Expect(v.Status.TLSConfigs[0].Name).Should(Equal(configs[0].Name))
			Expect(v.Status.TLSConfigs[1].Name).Should(Equal(configs[2].Name))
			Expect(v.Status.TLSConfigs[1].Secret).Should(Equal(configs[2].Secret))
		}
	})
})
