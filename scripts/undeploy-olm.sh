#!/bin/bash

# (c) Copyright [2021-2023] Open Text.
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

function usage() {
    echo "usage: $(basename $0)"
    exit 1
}

while getopts "h" opt
do
    case $opt in
        h) 
            usage
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

# The CSV is available in every namespace. We need to extract from it the
# namespace where the operator pod is running.
NAMESPACE=$(kubectl get csv -A -o jsonpath='{range .items[*]}{.metadata.annotations.olm\.operatorNamespace}{"\n"}{end}' | grep -v olm | head -1)

echo "Namespace: $NAMESPACE"

set -o xtrace

kubectl delete -n $NAMESPACE clusterserviceversion --selector operators.coreos.com/verticadb-operator.$NAMESPACE="" || :
kubectl delete -n $NAMESPACE operatorgroup e2e-operatorgroup || :
kubectl delete -n $NAMESPACE subscription e2e-verticadb-subscription || :
