/*
 (c) Copyright [2021-2023] Open Text.
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

package test

import (
	"fmt"

	gtypes "github.com/onsi/gomega/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EqualMetaV1Condition is a custom matcher to use for metav1.Condition
// that doesn't compare the LastTransitionTime
func EqualMetaV1Condition(expected interface{}) gtypes.GomegaMatcher {
	return &representMetaV1Condition{
		expected: expected,
	}
}

type representMetaV1Condition struct {
	expected interface{}
}

func (matcher *representMetaV1Condition) Match(actual interface{}) (success bool, err error) {
	response, ok := actual.(metav1.Condition)
	if !ok {
		return false, fmt.Errorf("representMetaV1Condition matcher expects a metav1.Condition")
	}

	expectedObj, ok := matcher.expected.(metav1.Condition)
	if !ok {
		return false, fmt.Errorf("representMetaV1Condition should compare with a metav1.Condition")
	}

	// Compare everything except lastTransitionTime
	return response.Type == expectedObj.Type &&
		response.Status == expectedObj.Status &&
		response.Reason == expectedObj.Reason, nil
}

func (matcher *representMetaV1Condition) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%#v\nto equal\n\t%#v", actual, matcher.expected)
}

func (matcher *representMetaV1Condition) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("Expected\n\t%#v\nto not equal\n\t%#v", actual, matcher.expected)
}
