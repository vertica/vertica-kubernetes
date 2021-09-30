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

# A script that will undeploy the operator that was previously deployed via OLM.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=30
NAMESPACE=$(kubectl config view --minify --output 'jsonpath={..namespace}')

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] <catalog_source_name>"
    echo
    echo "Options:"
    echo "  -n <namespace>  Check the webhook in this namespace."
    exit 1
}

while getopts "n:h" opt
do
    case $opt in
        n)
            NAMESPACE=$OPTARG
            ;;
        h) 
            usage
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

if [ $(( $# - $OPTIND )) -lt 0 ]
then
    usage
fi

CATALOG_SOURCE_NAME=${@:$OPTIND:1}

if [ -z "$NAMESPACE" ]
then
  NAMESPACE=default
fi

echo "Namespace: $NAMESPACE"

set -o xtrace

kubectl delete -n $NAMESPACE clusterserviceversion --selector operators.coreos.com/verticadb-operator.$NAMESPACE="" || :
kubectl delete -n $NAMESPACE operatorgroup e2e-operatorgroup || :
kubectl delete -n $NAMESPACE subscription e2e-verticadb-subscription || :
kubectl delete -n $NAMESPACE serviceaccount verticadb-operator-controller-manager || :
