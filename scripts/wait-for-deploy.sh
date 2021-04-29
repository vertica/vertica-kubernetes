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

GET_STS="kubectl get sts --selector=app.kubernetes.io/name=vertica"
if [ $(${GET_STS} | wc -l 2> /dev/null) -ge 2 ]
then
  while :
  do
      READY_REPLICAS=$(${GET_STS} -o 'jsonpath={.items[0].status.readyReplicas}')
      TOTAL_REPLICAS=$(${GET_STS} -o 'jsonpath={.items[0].status.replicas}')
      if [ -n "$READY_REPLICAS" ] && [ $READY_REPLICAS -eq $TOTAL_REPLICAS ]
      then
          ${GET_STS}
          exit 0
      fi
  done
fi
