#!/bin/sh

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

# Show the output of the currently running test
CUR_POD=$(kubectl get pod --selector=testing.kyma-project.io/created-by-octopus=true --field-selector=status.phase=Running -o=jsonpath='{.items[0].metadata.name}' 2> /dev/null)
if [ -z "$CUR_POD" ]
then
    echo "No octopus tests are running"
else
    kubectl logs -f $CUR_POD $@
fi
