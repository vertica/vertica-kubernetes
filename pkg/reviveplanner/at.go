/*
 (c) Copyright [2021-2023] Micro Focus or one of its affiliates.
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

package reviveplanner

import "github.com/go-logr/logr"

type ATPlanner struct {
	Database         Database
	CommunalLocation CommunalLocation
	Log              logr.Logger
	ParseComplete    bool
}

// MakeATPlanner is a factory function for the Planner interface. This makes one
// specific to admintools output.
func MakeATPlanner(log logr.Logger) Planner {
	return &ATPlanner{
		Log: log,
	}
}
