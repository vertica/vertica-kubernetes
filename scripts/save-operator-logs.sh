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

# This saves the output of the operators.  We run an operator for each test, so
# they will all be interleaved.  But each line of output starts with the pod
# name, so you can use sort to group all of the output for a single operator.

OP=${INT_TEST_OUTPUT_DIR:-int-tests-output}/verticadb-operator.log
mkdir -p $(dirname $OP)
echo "Saving operator logs to $OP"
stern --selector app.kubernetes.io/name=verticadb-operator --all-namespaces > $OP 2>&1
