/*
 (c) Copyright [2023-2024] Open Text.
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

package vclusterops

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const foundMismatchVers = "Found mismatched versions: "

func TestLogCheckVersionMatch(t *testing.T) {
	op := makeNMACheckVerticaVersionOp(nil, true, true)
	op.HasIncomingSCNames = true

	// case 1. one subcluster (enterprise db is one example of this case)
	// positive test
	op.SCToHostVersionMap[""] = hostVersionMap{
		"192.168.0.101": "Vertica Analytic Database v24.1.0",
		"192.168.0.102": "Vertica Analytic Database v24.1.0",
		"192.168.0.103": "Vertica Analytic Database v24.1.0",
	}
	err := op.logCheckVersionMatch()
	assert.NoError(t, err)
	// negative test
	op.SCToHostVersionMap = makeSCToHostVersionMap()
	op.SCToHostVersionMap["default_subcluster"] = hostVersionMap{
		"192.168.0.101": "Vertica Analytic Database v24.1.0",
		"192.168.0.102": "Vertica Analytic Database v24.1.0",
		"192.168.0.103": "Vertica Analytic Database v23.4.0",
	}
	err = op.logCheckVersionMatch()
	assert.Error(t, err)
	expectedErr1 := foundMismatchVers +
		"[Vertica Analytic Database v24.1.0] and [Vertica Analytic Database v23.4.0] in subcluster [default_subcluster]"
	expectedErr2 := foundMismatchVers +
		"[Vertica Analytic Database v23.4.0] and [Vertica Analytic Database v24.1.0] in subcluster [default_subcluster]"
	isExpected := strings.Contains(err.Error(), expectedErr1) || strings.Contains(err.Error(), expectedErr2)
	assert.Equal(t, true, isExpected)

	// case 2. multiple subclusters
	// positive test
	op.SCToHostVersionMap = makeSCToHostVersionMap()
	op.SCToHostVersionMap["default_subcluster"] = hostVersionMap{
		"192.168.0.101": "Vertica Analytic Database v24.1.0",
		"192.168.0.102": "Vertica Analytic Database v24.1.0",
		"192.168.0.103": "Vertica Analytic Database v24.1.0",
	}
	op.SCToHostVersionMap["sc1"] = hostVersionMap{
		"192.168.0.104": "Vertica Analytic Database v23.4.0",
		"192.168.0.105": "Vertica Analytic Database v23.4.0",
		"192.168.0.106": "Vertica Analytic Database v23.4.0",
	}
	op.SCToHostVersionMap["sc2"] = hostVersionMap{
		"192.168.0.107": "Vertica Analytic Database v23.3.0",
		"192.168.0.108": "Vertica Analytic Database v23.3.0",
		"192.168.0.109": "Vertica Analytic Database v23.3.0",
	}
	err = op.logCheckVersionMatch()
	assert.NoError(t, err)
	// negative test 1
	op.SCToHostVersionMap = makeSCToHostVersionMap()
	op.SCToHostVersionMap["default_subcluster"] = hostVersionMap{
		"192.168.0.101": "Vertica Analytic Database v24.1.0",
		"192.168.0.102": "Vertica Analytic Database v24.1.0",
		"192.168.0.103": "Vertica Analytic Database v24.1.0",
	}
	op.SCToHostVersionMap["sc1"] = hostVersionMap{
		"192.168.0.104": "Vertica Analytic Database v23.4.0",
		"192.168.0.105": "Vertica Analytic Database v23.4.0",
		"192.168.0.106": "Vertica Analytic Database v23.4.0",
	}
	op.SCToHostVersionMap["sc2"] = hostVersionMap{
		"192.168.0.107": "Vertica Analytic Database v23.4.0",
		"192.168.0.108": "Vertica Analytic Database v23.3.0",
		"192.168.0.109": "Vertica Analytic Database v23.4.0",
	}
	err = op.logCheckVersionMatch()
	assert.Error(t, err)
	expectedErr1 = foundMismatchVers +
		"[Vertica Analytic Database v23.4.0] and [Vertica Analytic Database v23.3.0] in subcluster [sc2]"
	expectedErr2 = foundMismatchVers +
		"[Vertica Analytic Database v23.3.0] and [Vertica Analytic Database v23.4.0] in subcluster [sc2]"
	isExpected = strings.Contains(err.Error(), expectedErr1) || strings.Contains(err.Error(), expectedErr2)
	assert.Equal(t, true, isExpected)

	// case 3: no version found in the nodes
	// no version found for one node
	op.SCToHostVersionMap = makeSCToHostVersionMap()
	op.SCToHostVersionMap["default_subcluster"] = hostVersionMap{
		"192.168.0.101": "Vertica Analytic Database v24.1.0",
		"192.168.0.102": "Vertica Analytic Database v24.1.0",
		"192.168.0.103": "",
	}
	op.SCToHostVersionMap["sc1"] = hostVersionMap{
		"192.168.0.104": "Vertica Analytic Database v23.4.0",
		"192.168.0.105": "Vertica Analytic Database v23.4.0",
		"192.168.0.106": "Vertica Analytic Database v23.4.0",
	}
	err = op.logCheckVersionMatch()
	assert.ErrorContains(t, err, "No version collected for host [192.168.0.103] in subcluster [default_subcluster]")
	// no version found for all the nodes in a subcluster
	op.SCToHostVersionMap = makeSCToHostVersionMap()
	op.SCToHostVersionMap["default_subcluster"] = hostVersionMap{
		"192.168.0.101": "Vertica Analytic Database v24.1.0",
		"192.168.0.102": "Vertica Analytic Database v24.1.0",
		"192.168.0.103": "Vertica Analytic Database v24.1.0",
	}
	op.SCToHostVersionMap["sc1"] = hostVersionMap{}
	err = op.logCheckVersionMatch()
	assert.ErrorContains(t, err, "No version collected for all hosts in subcluster [sc1]")
}
