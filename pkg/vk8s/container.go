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

package vk8s

import (
	"errors"

	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
)

// GetServerContainer returns a pointer to the container that runs the Vertica
// server
func GetServerContainer(cnts []corev1.Container) *corev1.Container {
	return getNamedContainer(cnts, names.ServerContainer)
}

// GetNMAContainer returns a pointer to the container that runs the node
// management agent.
func GetNMAContainer(cnts []corev1.Container) *corev1.Container {
	return getNamedContainer(cnts, names.NMAContainer)
}

// GetScrutinizeInitContainerStatus returns a pointer to the status of the
// scrutinize init container
func GetScrutinizeInitContainerStatus(cntStatuses []corev1.ContainerStatus) *corev1.ContainerStatus {
	return getNamedContainerStatus(cntStatuses, names.ScrutinizeInitContainer)
}

// GetNMAContainerStatus returns a pointer to the status of the NMA container
func GetNMAContainerStatus(cntStatuses []corev1.ContainerStatus) *corev1.ContainerStatus {
	return getNamedContainerStatus(cntStatuses, names.NMAContainer)
}

func getNamedContainer(cnts []corev1.Container, cntName string) *corev1.Container {
	for i := range cnts {
		if cnts[i].Name == cntName {
			return &cnts[i]
		}
	}
	return nil
}

func getNamedContainerStatus(cntStatuses []corev1.ContainerStatus, cntName string) *corev1.ContainerStatus {
	for i := range cntStatuses {
		if cntStatuses[i].Name == cntName {
			return &cntStatuses[i]
		}
	}
	return nil
}

// GetServerImage returns the name of the image used for the server container.
// It returns an error if it cannot find the server container.
func GetServerImage(cnts []corev1.Container) (string, error) {
	cnt := GetServerContainer(cnts)
	if cnt == nil {
		return "", errors.New("could not find the server container")
	}
	return cnt.Image, nil
}

// HasNMAContainer returns true if the given container spec has the NMA
// sidecar container.
func HasNMAContainer(podSpec *corev1.PodSpec) bool {
	return GetNMAContainer(podSpec.Containers) != nil
}

// IsNMAContainerReady returns true if the NMA container has its status
// in the given pod status and is in ready state
func IsNMAContainerReady(podStatus *corev1.PodStatus) bool {
	cntStatus := GetNMAContainerStatus(podStatus.ContainerStatuses)
	if cntStatus == nil {
		return false
	}
	return cntStatus.Ready
}
