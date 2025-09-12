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
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("ObservedConfigObjsReconciler", func() {
	var vdb *vapi.VerticaDB
	var reconciler *ObservedConfigObjsReconciler

	BeforeEach(func() {
		vdb = vapi.MakeVDB()
		reconciler = &ObservedConfigObjsReconciler{
			Vdb: vdb,
		}
	})

	It("should return empty slice if no resources are referenced", func() {
		Expect(reconciler.getReferencedResources(true)).To(BeEmpty())
		Expect(reconciler.getReferencedResources(false)).To(BeEmpty())
	})

	It("should return secret names from ExtraEnv with SecretKeyRef when isSecret is true", func() {
		vdb.Spec.ExtraEnv = []corev1.EnvVar{
			{
				Name: "SECRET_ENV",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
						Key:                  "password",
					},
				},
			},
		}
		Expect(reconciler.getReferencedResources(true)).To(ConsistOf("my-secret"))
	})

	It("should return configmap names from ExtraEnv with ConfigMapKeyRef when isSecret is false", func() {
		vdb.Spec.ExtraEnv = []corev1.EnvVar{
			{
				Name: "CM_ENV",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
						Key:                  "cmkey",
					},
				},
			},
		}
		Expect(reconciler.getReferencedResources(false)).To(ConsistOf("my-cm"))
	})

	It("should return secret names from EnvFrom with SecretRef when isSecret is true", func() {
		vdb.Spec.EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "envfrom-secret"},
				},
			},
		}
		Expect(reconciler.getReferencedResources(true)).To(ConsistOf("envfrom-secret"))
	})

	It("should return configmap names from EnvFrom with ConfigMapRef when isSecret is false", func() {
		vdb.Spec.EnvFrom = []corev1.EnvFromSource{
			{
				ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "envfrom-cm"},
				},
			},
		}
		Expect(reconciler.getReferencedResources(false)).To(ConsistOf("envfrom-cm"))
	})

	It("should return unique resource names from both ExtraEnv and EnvFrom", func() {
		vdb.Spec.ExtraEnv = []corev1.EnvVar{
			{
				Name: "SECRET_ENV1",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
						Key:                  "key1",
					},
				},
			},
			{
				Name: "SECRET_ENV2",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "secret2"},
						Key:                  "key2",
					},
				},
			},
		}
		vdb.Spec.EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "secret2"},
				},
			},
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "secret3"},
				},
			},
		}
		Expect(reconciler.getReferencedResources(true)).To(ConsistOf("secret1", "secret2", "secret3"))
	})

	It("should ignore configmap references when isSecret is true", func() {
		vdb.Spec.ExtraEnv = []corev1.EnvVar{
			{
				Name: "CM_ENV",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
						Key:                  "cmkey",
					},
				},
			},
		}
		vdb.Spec.EnvFrom = []corev1.EnvFromSource{
			{
				ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "envfrom-cm"},
				},
			},
		}
		Expect(reconciler.getReferencedResources(true)).To(BeEmpty())
	})

	It("should ignore secret references when isSecret is false", func() {
		vdb.Spec.ExtraEnv = []corev1.EnvVar{
			{
				Name: "SECRET_ENV",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
						Key:                  "password",
					},
				},
			},
		}
		vdb.Spec.EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "envfrom-secret"},
				},
			},
		}
		Expect(reconciler.getReferencedResources(false)).To(BeEmpty())
	})

	It("should handle mixed ExtraEnv and EnvFrom with both secret and configmap references", func() {
		vdb.Spec.ExtraEnv = []corev1.EnvVar{
			{
				Name: "SECRET_ENV",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "secret2"},
						Key:                  "key2",
					},
				},
			},
			{
				Name: "CM_ENV",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "cm2"},
						Key:                  "cmkey",
					},
				},
			},
		}
		vdb.Spec.EnvFrom = []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "secret1"},
				},
			},
			{
				ConfigMapRef: &corev1.ConfigMapEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "cm1"},
				},
			},
		}
		Expect(reconciler.getReferencedResources(true)).To(ConsistOf("secret1", "secret2"))
		Expect(reconciler.getReferencedResources(false)).To(ConsistOf("cm1", "cm2"))
	})

	It("equalSets should return true for two empty slices", func() {
		Expect(equalSets(nil, []string{})).To(BeTrue())
	})

	It("equalSets should return true for slices with same elements in same order", func() {
		Expect(equalSets([]string{"a", "b", "c"}, []string{"a", "b", "c"})).To(BeTrue())
	})

	It("equalSets should return true for slices with same elements in different order", func() {
		Expect(equalSets([]string{"a", "b", "c"}, []string{"c", "a", "b"})).To(BeTrue())
	})

	It("equalSets should return false for slices with different lengths", func() {
		Expect(equalSets([]string{"a", "b"}, []string{"a", "b", "c"})).To(BeFalse())
	})

	It("equalSets should return false for slices with different elements", func() {
		Expect(equalSets([]string{"a", "b", "c"}, []string{"a", "b", "d"})).To(BeFalse())
	})

	It("equalSets should return false if one slice has duplicates and the other does not", func() {
		Expect(equalSets([]string{"a", "b", "b"}, []string{"a", "b"})).To(BeFalse())
	})

	It("equalSets should return true for slices with same elements and duplicates in both", func() {
		Expect(equalSets([]string{"a", "b", "b"}, []string{"b", "a", "b"})).To(BeTrue())
	})
})
