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

# Delete the PVC to trigger removal of the data.   We do thes because when a
# stateful set is deleted the PVC sticks around to retain the storage.

PVCS=$(kubectl --selector=app.kubernetes.io/name=vertica get pvc | tail -n +2 | awk '{print $1}')
kubectl delete pvc $PVCS