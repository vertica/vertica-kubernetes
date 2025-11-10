#!/bin/bash

# (c) Copyright [2021-2024] Open Text.
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

# A script that will clean up left over resources of Prometheus.

NAMESPACE=$1
if [ -z "$NAMESPACE" ]
then
  NAMESPACE=default
fi

function remove_prometheus_leftover
{
    NAMESPACE=$1
    echo "Clean up Prometheus left over resources for ns: $NAMESPACE"
    for obj in clusterrole clusterrolebinding mutatingwebhookconfigurations validatingwebhookconfigurations
    do
        if kubectl -n $NAMESPACE get $obj | grep '^prometheus-kube-'
        then
            kubectl delete -n $NAMESPACE $obj $(kubectl -n $NAMESPACE get $obj | grep '^prometheus-kube-' | cut -d' ' -f1) || true
        fi
    done
    if kubectl -n kube-system get svc | grep '^prometheus-kube-'
    then
        kubectl -n kube-system delete svc $(kubectl -n kube-system get svc | grep '^prometheus-kube-' | cut -d' ' -f1) || true
    fi
}

remove_prometheus_leftover $NAMESPACE
