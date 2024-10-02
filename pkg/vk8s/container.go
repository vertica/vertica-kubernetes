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

// GetScrutinizeInitContainer returns a pointer to the container that runs scrutinize
func GetScrutinizeInitContainer(cnts []corev1.Container) *corev1.Container {
	return getNamedContainer(cnts, names.ScrutinizeInitContainer)
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

// SetVerticaImage sets the image used for the server and nma containers.
// It returns an error if it cannot find any of the containers.
func SetVerticaImage(cnts []corev1.Container, image string, includeNMA bool) error {
	cnt := GetServerContainer(cnts)
	if cnt == nil {
		return errors.New("could not find the server container")
	}
	cnt.Image = image
	if includeNMA {
		cnt = GetNMAContainer(cnts)
		if cnt == nil {
			return errors.New("could not find the nma container")
		}
		cnt.Image = image
	}
	return nil
}

// FindNMAContainerStatus will return the status of the NMA container if available.
func FindNMAContainerStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	return findContainerStatus(pod.Status.ContainerStatuses, names.NMAContainer)
}

// FindNMAContainerStatus will return the status of the server container
func FindServerContainerStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	return findContainerStatus(pod.Status.ContainerStatuses, names.ServerContainer)
}

// FindRunningPodWithNMAContainer finds a running pod with NMA ready.
func FindRunningPodWithNMAContainer(pods *corev1.PodList) string {
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase == corev1.PodRunning {
			if IsNMAContainerReady(pod) {
				return pod.Status.PodIP
			}
		}
	}
	return ""
}

// FindScrutinizeInitContainerStatus will return the status of the scrutinize
// init container
func FindScrutinizeInitContainerStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	return findContainerStatus(pod.Status.InitContainerStatuses, names.ScrutinizeInitContainer)
}

// findContainerStatus is a helper to return status for a named container
func findContainerStatus(cntStatuses []corev1.ContainerStatus, containerName string) *corev1.ContainerStatus {
	for i := range cntStatuses {
		if cntStatuses[i].Name == containerName {
			return &cntStatuses[i]
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

// IsNMAContainerReady returns true if the NMA container has its status
// in the given pod status and is in ready state
func IsNMAContainerReady(pod *corev1.Pod) bool {
	cntStatus := FindNMAContainerStatus(pod)
	if cntStatus == nil {
		return false
	}
	return cntStatus.Ready
}
