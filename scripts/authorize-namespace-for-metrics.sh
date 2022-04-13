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
NAMESPACE=$1
SA=verticadb-operator-controller-manager

if [ -z $NAMESPACE ]
then
    echo "*** Must specify a namespace"
    exit 1
fi

set -o xtrace

kubectl apply -f $REPO_DIR/config/release-manifests/verticadb-operator-proxy-role-cr.yaml

set +o errexit
kubectl create clusterrolebinding verticadb-operator-proxy-rolebinding --clusterrole=verticadb-operator-proxy-role --serviceaccount=$NAMESPACE:$SA
RES=$?
set -o errexit

# Append to ClusterRoleBinding if it already exists
if [ $RES -ne "0" ]
then
  tmpfile=$(mktemp /tmp/patch-XXXXXX.yaml)
  trap "rm $tmpfile" 0 2 3 15   # Ensure deletion on script exit
  cat <<- EOF > $tmpfile
  [{"op": "add",
    "path": "/subjects/-",
    "value":
      {"kind": "ServiceAccount",
       "name": "$SA",
       "namespace": "$NAMESPACE"}
  }]
EOF
  kubectl patch clusterrolebinding verticadb-operator-proxy-rolebinding --type='json' --patch-file $tmpfile
fi

