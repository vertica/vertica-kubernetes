#!/bin/bash

# (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

# A script that will authorize the given service account in the given namespace
# the ability to access a protected metrics endpoint.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
OP_SA=verticadb-operator-controller-manager

function usage() {
    echo "usage: $0 [<op_namespace>] [<access_namespace>] [<access_serviceaccount>]"
    echo
    echo "Positional Arguments:"
    echo " <op_namespace>           The namespace that runs the operator"
    echo " <access_namespace>       The namespace that will run the pod that will issue the /metrics REST call"
    echo " <access_serviceaccount>  The ServiceAccount of the pod that will issue the /metrics REST call"
    exit 1
}

if [ $(( $# - $OPTIND )) -lt 2 ]
then
    usage
fi

OP_NAMESPACE=$1
ACCESS_NAMESPACE=$2
ACCESS_SA=$3

set -o xtrace

kubectl apply -f $REPO_DIR/config/release-manifests/verticadb-operator-proxy-role-cr.yaml
kubectl apply -f $REPO_DIR/config/release-manifests/verticadb-operator-metrics-reader-cr.yaml

set +o errexit
kubectl create clusterrolebinding verticadb-operator-proxy-rolebinding --clusterrole=verticadb-operator-proxy-role --serviceaccount=$OP_NAMESPACE:$OP_SA
RES=$?
set -o errexit

# Append to ClusterRoleBinding if it already exists
if [ $RES -ne "0" ]
then
  tmpfile=$(mktemp /tmp/patch-XXXXXX.yaml)
  cat <<- EOF > $tmpfile
  [{"op": "add",
    "path": "/subjects/-",
    "value":
      {"kind": "ServiceAccount",
       "name": "$OP_SA",
       "namespace": "$OP_NAMESPACE"}
  }]
EOF
  kubectl patch clusterrolebinding verticadb-operator-proxy-rolebinding --type='json' --patch-file $tmpfile
  rm $tmpfile
fi

set +o errexit
kubectl create clusterrolebinding verticadb-operator-metrics-reader --clusterrole=verticadb-operator-metrics-reader --serviceaccount=$ACCESS_NAMESPACE:$ACCESS_SA
RES=$?
set -o errexit

# Append to ClusterRoleBinding if it already exists
if [ $RES -ne "0" ]
then
  tmpfile=$(mktemp /tmp/patch-XXXXXX.yaml)
  cat <<- EOF > $tmpfile
  [{"op": "add",
    "path": "/subjects/-",
    "value":
      {"kind": "ServiceAccount",
       "name": "$ACCESS_SA",
       "namespace": "$ACCESS_NAMESPACE"}
  }]
EOF
  kubectl patch clusterrolebinding verticadb-operator-metrics-reader --type='json' --patch-file $tmpfile
  rm $tmpfile
fi
