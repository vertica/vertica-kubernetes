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

package vstruct

type NullableBool int

const (
	False NullableBool = iota
	True
	NotSet
)

func (e NullableBool) ToBool() bool {
	return e == True
}

func (e *NullableBool) FromBoolPointer(val *bool) {
	switch {
	case val == nil:
		*e = NotSet
	case *val:
		*e = True
	default:
		*e = False
	}
}

func MakeNullableBool(val bool) (e NullableBool) {
	if val {
		e = True
	} else {
		e = False
	}
	return e
}
