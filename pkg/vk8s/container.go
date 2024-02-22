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
	"strings"

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

func getNamedContainer(cnts []corev1.Container, cntName string) *corev1.Container {
	for i := range cnts {
		if cnts[i].Name == cntName {
			return &cnts[i]
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

// FindNMAContainerStatus will return the status of the NMA container if available.
func FindNMAContainerStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	return findContainerStatus(pod, names.NMAContainer)
}

// FindNMAContainerStatus will return the status of the server container
func FindServerContainerStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	return findContainerStatus(pod, names.ServerContainer)
}

// findContainerStatus is a helper to return status for a named container
func findContainerStatus(pod *corev1.Pod, containerName string) *corev1.ContainerStatus {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == containerName {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
}

// HasCreateContainerError returns true if the container cannot start because
// there is no command specified. This can happen if the image doesn't have a
// command to automatically run. This typically means the image is designed for 24.2.0+.
func HasCreateContainerError(containerStatus *corev1.ContainerStatus) bool {
	return containerStatus.State.Waiting != nil &&
		containerStatus.State.Waiting.Reason == "CreateContainerError" &&
		strings.Contains(containerStatus.State.Waiting.Message, "no command specified")
}

// HasNMAContainer returns true if the given container spec has the NMA
// sidecar container.
func HasNMAContainer(podSpec *corev1.PodSpec) bool {
	return GetNMAContainer(podSpec.Containers) != nil
}
