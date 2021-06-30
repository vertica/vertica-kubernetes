#!/bin/bash

# (c) Copyright [2021] Micro Focus or one of its affiliates.
# Licensed under the Apache License, Version 2.0 (the "License");
# You may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This saves the log of any pod running vertica

OP=${INT_TEST_OUTPUT_DIR:-int-tests-output}/vertica.log
mkdir -p $(dirname $OP)
echo "Saving vertica logs to $OP"
stern --selector app.kubernetes.io/name=vertica --all-namespaces --timestamps > $OP 2>&1
