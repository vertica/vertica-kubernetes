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

import "strings"

// isIPv6 decides whether an IP string is ipv6
func isIPv6(address string) bool {
	const minNumberColonInIPv6 = 2
	return strings.Count(address, ":") >= minNumberColonInIPv6
}

// podsAllHaveIPv6 decides whether all pods in the cluster contain IPv6 addresses
func podsAllHaveIPv6(pods []*PodFact) bool {
	ipv6Count := 0
	for _, pod := range pods {
		if isIPv6(pod.podIP) {
			ipv6Count++
		}
	}
	return ipv6Count > 0 && ipv6Count == len(pods)
}
