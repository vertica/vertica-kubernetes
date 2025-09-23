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

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cache"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("servicemonitor_reconciler", func() {
	ctx := context.Background()

	It("should create ServiceMonitor if it does not exist", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		rec := &ServiceMonitorReconciler{
			VRec:         vdbRec,
			Vdb:          vdb,
			Log:          logger,
			CacheManager: cache.MakeCacheManager(true),
		}

		// Ensure ServiceMonitor does not exist
		svcMonName := names.GenSvcMonitorName(vdb)
		svcMon := &monitoringv1.ServiceMonitor{}
		err := k8sClient.Get(ctx, svcMonName, svcMon)
		Expect(err).To(HaveOccurred())

		// Call reconcileServiceMonitor
		Expect(rec.reconcileServiceMonitor(ctx)).Should(Succeed())

		// ServiceMonitor should now exist
		err = k8sClient.Get(ctx, svcMonName, svcMon)
		defer func() { _ = k8sClient.Delete(ctx, svcMon) }()
		Expect(err).Should(Succeed())
	})

	It("should update ServiceMonitor if it already exists", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		rec := &ServiceMonitorReconciler{
			VRec:         vdbRec,
			Vdb:          vdb,
			Log:          logger,
			CacheManager: cache.MakeCacheManager(true),
		}

		// Pre-create ServiceMonitor
		secName := names.GenBasicauthSecretName(vdb)
		smName := names.GenSvcMonitorName(vdb)
		expSvcMon := builder.BuildServiceMonitor(smName, vdb, secName.Name)
		Expect(k8sClient.Create(ctx, expSvcMon)).Should(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, expSvcMon) }()

		// Call reconcileServiceMonitor
		Expect(rec.reconcileServiceMonitor(ctx)).Should(Succeed())
	})

	It("should return error if ServiceMonitor Create fails", func() {
		// Use a Vdb with an invalid namespace to force Create error
		vdb := vapi.MakeVDB()
		vdb.Namespace = "nonexistent-ns"
		rec := &ServiceMonitorReconciler{
			VRec:         vdbRec,
			Vdb:          vdb,
			Log:          logger,
			CacheManager: cache.MakeCacheManager(true),
		}
		Expect(rec.reconcileServiceMonitor(ctx)).ShouldNot(Succeed())
	})

	It("should do nothing if Vdb is set for TLS", func() {
		vdb := vapi.MakeVDBForTLS()
		rec := &ServiceMonitorReconciler{
			VRec:         vdbRec,
			Vdb:          vdb,
			Log:          logger,
			CacheManager: cache.MakeCacheManager(true),
		}
		Expect(rec.reconcileBasicAuth(ctx)).Should(Succeed())
	})

	It("should create basic auth secret if it does not exist and PasswordSecret is empty", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		rec := &ServiceMonitorReconciler{
			VRec:         vdbRec,
			Vdb:          vdb,
			Log:          logger,
			CacheManager: cache.MakeCacheManager(true),
		}
		rec.CacheManager.InitCacheForVdb(vdb, nil)
		secName := names.GenBasicauthSecretName(vdb)
		sec := &corev1.Secret{}
		_ = k8sClient.Delete(ctx, sec) // Ensure it doesn't exist

		Expect(rec.reconcileBasicAuth(ctx)).Should(Succeed())
		Expect(k8sClient.Get(ctx, secName, sec)).Should(Succeed())
		_ = k8sClient.Delete(ctx, sec)
	})

	It("should create basic auth secret with password if PasswordSecret is set", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.PasswordSecret = "pass"
		test.CreateVDB(ctx, k8sClient, vdb)
		vdb.Status.PasswordSecret = &vdb.Spec.PasswordSecret
		test.CreateSuperuserPasswordSecret(ctx, vdb, k8sClient, *vdb.Status.PasswordSecret, "xxxx")
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		defer test.DeleteSecret(ctx, k8sClient, *vdb.Status.PasswordSecret)
		rec := &ServiceMonitorReconciler{
			VRec:         vdbRec,
			Vdb:          vdb,
			Log:          logger,
			CacheManager: cache.MakeCacheManager(true),
		}
		rec.CacheManager.InitCacheForVdb(vdb, nil)
		secName := names.GenBasicauthSecretName(vdb)
		sec := &corev1.Secret{}
		_ = k8sClient.Delete(ctx, sec) // Ensure it doesn't exist

		Expect(rec.reconcileBasicAuth(ctx)).Should(Succeed())
		Expect(k8sClient.Get(ctx, secName, sec)).Should(Succeed())
		_ = k8sClient.Delete(ctx, sec)
	})

	It("should return error if Secret Create fails", func() {
		vdb := vapi.MakeVDB()
		vdb.Namespace = "nonexistent-ns"
		rec := &ServiceMonitorReconciler{
			VRec:         vdbRec,
			Vdb:          vdb,
			Log:          logger,
			CacheManager: cache.MakeCacheManager(true),
		}
		rec.CacheManager.InitCacheForVdb(vdb, nil)
		Expect(rec.reconcileBasicAuth(ctx)).ShouldNot(Succeed())
	})

	It("should not create secret if it already exists", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		rec := &ServiceMonitorReconciler{
			VRec:         vdbRec,
			Vdb:          vdb,
			Log:          logger,
			CacheManager: cache.MakeCacheManager(true),
		}
		secName := names.GenBasicauthSecretName(vdb)
		sec := builder.BuildBasicAuthSecret(vdb, secName.Name, vdb.GetVerticaUser(), testPassword)
		Expect(k8sClient.Create(ctx, sec)).Should(Succeed())
		defer func() { _ = k8sClient.Delete(ctx, sec) }()

		Expect(rec.reconcileBasicAuth(ctx)).Should(Succeed())
	})

})
