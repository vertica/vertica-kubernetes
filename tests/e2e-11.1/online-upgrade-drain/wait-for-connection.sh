#!/bin/bash

# (c) Copyright [2022] Micro Focus or one of its affiliates.
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

set -o errexit

NAMESPACE=$1
SC_TYPE=$2
POD=test-long-running-connection-$SC_TYPE

while ! kubectl get pod -n $NAMESPACE $POD 2> /dev/null; do sleep 0.1; done
echo "Waiting for pod to be in ready state..."
kubectl wait -n $NAMESPACE --for=condition=Ready=True pod $POD --timeout 600s
echo "Waiting for vsql connection..."
kubectl exec -i -n $NAMESPACE $POD -- bash -c "while [ ! -f /tmp/connected ]; do sleep 3; done"
