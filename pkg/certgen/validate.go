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

package certgen

import (
	"fmt"
	"net"
	"strings"
)

// ParseAndValidateIPs parse a comma-separated string and returns
// the ip list
func ParseAndValidateIPs(ipsString string) ([]net.IP, error) {
	if ipsString == "" {
		// No IPs provided is also a valid case (empty list)
		return nil, nil
	}

	ipList := strings.Split(ipsString, ",")
	validIPs := make([]net.IP, 0, len(ipList))
	var invalidIPs []string

	for _, ipStr := range ipList {
		trimmedIP := strings.TrimSpace(ipStr)
		ip := net.ParseIP(trimmedIP)
		if ip == nil {
			invalidIPs = append(invalidIPs, trimmedIP)
		} else {
			validIPs = append(validIPs, ip)
		}
	}

	if len(invalidIPs) > 0 {
		return nil, fmt.Errorf("the following are not valid IP addresses: %s", strings.Join(invalidIPs, ", "))
	}

	return validIPs, nil
}
