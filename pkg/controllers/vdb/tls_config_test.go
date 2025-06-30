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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/test"
)

var _ = Describe("tls_config", func() {
	ctx := context.Background()

	const (
		disable   = "disable"
		tryVerify = "try_verify"
		newSecret = "new-secret"
		oldSecret = "old-secret"
	)

	It("should be a no-op if no TLS update is needed", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		recon := MakeTLSConfigManager(vdbRec, logger, vdb, "HTTP", dispatcher)

		recon.CurrentSecret = "secret1"
		recon.NewSecret = "secret1"
		recon.CurrentTLSMode = disable
		recon.NewTLSMode = disable
		recon.setTLSUpdateType()

		Expect(recon.needTLSConfigChange()).To(BeFalse())
	})

	It("should detect TLS mode and cert rotation", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		fpr := &cmds.FakePodRunner{}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		manager := MakeTLSConfigManager(vdbRec, logger, vdb, tlsConfigHTTPS, dispatcher)

		manager.CurrentSecret = oldSecret
		manager.NewSecret = newSecret
		manager.CurrentTLSMode = disable
		manager.NewTLSMode = tryVerify
		manager.setTLSUpdateType()

		Expect(manager.TLSUpdateType).To(Equal(tlsModeAndCertChange))
	})

	It("should detect TLS mode change", func() {
		vdb := vapi.MakeVDB()
		fpr := &cmds.FakePodRunner{}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		manager := MakeTLSConfigManager(vdbRec, logger, vdb, tlsConfigHTTPS, dispatcher)

		manager.CurrentSecret = newSecret
		manager.NewSecret = newSecret
		manager.CurrentTLSMode = disable
		manager.NewTLSMode = tryVerify
		manager.setTLSUpdateType()

		manager.setTLSUpdateType()
		Expect(manager.TLSUpdateType).To(Equal(tlsModeChangeOnly))
	})

	It("should return valid k8s certs config", func() {
		vdb := vapi.MakeVDB()
		tlsMgr := MakeTLSConfigManager(vdbRec, logger, vdb, tlsConfigServer, nil)

		cacheDuration := fmt.Sprintf(",\"cache-duration\":%d", 10)
		keyConfig, certConfig, caCertConfig := tlsMgr.getK8sCertsConfig(cacheDuration)
		Expect(keyConfig).To(ContainSubstring("data-key"))
		Expect(certConfig).To(ContainSubstring("data-key"))
		Expect(caCertConfig).To(ContainSubstring("data-key"))
		Expect(keyConfig).To(ContainSubstring("cache-duration"))
		Expect(certConfig).To(ContainSubstring("cache-duration"))
		Expect(caCertConfig).To(ContainSubstring("cache-duration"))
	})

	It("should parse TLS config from DB correctly", func() {
		manager := MakeTLSConfigManager(vdbRec, logger, vapi.MakeVDB(), "HTTP", nil)
		out := "https_cert_abc|try_verify"

		cert, mode, err := manager.parseConfig(out)

		Expect(err).To(BeNil())
		Expect(cert).To(Equal("https_cert_abc"))
		Expect(mode).To(Equal(tryVerify))
	})

	It("should return HTTPS events", func() {
		mgr := &TLSConfigManager{
			TLSConfig: tlsConfigHTTPS,
		}

		start, fail, success := mgr.getEvents()
		Expect(start).To(Equal(events.HTTPSTLSUpdateStarted))
		Expect(fail).To(Equal(events.HTTPSTLSUpdateFailed))
		Expect(success).To(Equal(events.HTTPSTLSUpdateSucceeded))
	})

	It("should set tls update data correctly", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPSNMATLSSecret = newSecret
		vdb.Spec.HTTPSTLSMode = tryVerify

		mgr := &TLSConfigManager{
			TLSConfig: tlsConfigHTTPS,
			Vdb:       vdb,
		}

		mgr.setTLSUpdatedata()

		Expect(mgr.CurrentSecret).To(Equal(""))
		Expect(mgr.NewSecret).To(Equal(newSecret))
		Expect(mgr.CurrentTLSMode).To(Equal(""))
		Expect(mgr.NewTLSMode).To(Equal(tryVerify))
		Expect(mgr.secretType).To(Equal(vapi.HTTPSTLSSecretType))
		Expect(mgr.tlsModeType).To(Equal(vapi.HTTPSTLSModeType))

		vdb.Status.SecretRefs = []vapi.SecretRef{
			{
				Name: oldSecret,
				Type: vapi.HTTPSTLSSecretType,
			},
		}
		vdb.Status.TLSModes = []vapi.TLSMode{
			{
				Mode: tryVerify,
				Type: vapi.HTTPSTLSModeType,
			},
		}

		mgr.setTLSUpdatedata()

		Expect(mgr.CurrentSecret).To(Equal(oldSecret))
		Expect(mgr.NewSecret).To(Equal(newSecret))
		Expect(mgr.CurrentTLSMode).To(Equal(tryVerify))
		Expect(mgr.NewTLSMode).To(Equal(tryVerify))
		Expect(mgr.secretType).To(Equal(vapi.HTTPSTLSSecretType))
		Expect(mgr.tlsModeType).To(Equal(vapi.HTTPSTLSModeType))
	})

})
