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

# A script that will undeploy the operator.  It will attempt to discover how
# wether helm or olm was used to deploy and take the appropriate action.

set -o errexit
set -o pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=30
HELM_RELEASE_NAME=$(grep 'HELM_RELEASE_NAME?=' $REPO_DIR/Makefile | cut -d'=' -f2)

function usage() {
    echo "usage: $(basename $0) [-e <helm_release_name>] [-i]"
    echo
    echo "Options:"
    echo "  -e <helm_release_name>  Name of the helm release to look for and undeploy if present."
    echo "  -i                      Ignore, and don't fail, when deployment isn't present."
    exit 1
}

while getopts "hi" opt
do
    case $opt in
        h) 
            usage
            ;;
        e)
            HELM_RELEASE_NAME=$OPTARG
            ;;
        i)
            IGNORE_NOT_FOUND=1
            ;;
        \?)
            echo "ERROR: unrecognized option: -$opt"
            usage
            ;;
    esac
done

function remove_cluster_objects
{
    # Sometimes cluster scoped operator can stick around after removing the
    # helm chart or OLM deployment. This ensures they go away.
    kubectl delete clusterrole $(kubectl get clusterrole | grep '^verticadb-operator-' | cut -d' ' -f1) || :
    kubectl delete clusterrolebinding $(kubectl get clusterrolebinding | grep '^verticadb-operator-' | cut -d' ' -f1) || :
    kubectl delete mutatingwebhookconfigurations $(kubectl get mutatingwebhookconfigurations | grep '^verticadb-operator-' | cut -d' ' -f1) || :
    kubectl delete validatingwebhookconfigurations $(kubectl get validatingwebhookconfigurations | grep '^verticadb-operator-' | cut -d' ' -f1) || :
}

set -o xtrace

if kubectl get clusterserviceversion | grep -cqe "VerticaDB Operator" 2> /dev/null
then
    $SCRIPT_DIR/undeploy-olm.sh
    remove_cluster_objects
elif helm list --all-namespaces --filter $HELM_RELEASE_NAME --no-headers | grep -q $HELM_RELEASE_NAME
then
    NS=$(helm list --all-namespaces --filter vdb-op --output json | jq -r '[.[].namespace][0]')
    helm uninstall -n $NS $HELM_RELEASE_NAME
    remove_cluster_objects
else
    echo "** No operator deployment detected"
    if [ -n "$IGNORE_NOT_FOUND" ]
    then
        exit 0
    fi
    exit 1
fi
