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
    set +o xtrace
    # Sometimes cluster scoped operator can stick around after removing the
    # helm chart or OLM deployment. This can happen if you don't uninstall the
    # release, but instead delete the namespace where the release is located.
    # This ensures that we properly clean those up.
    for obj in clusterrole clusterrolebinding mutatingwebhookconfigurations validatingwebhookconfigurations
    do
        if kubectl get $obj | grep '^verticadb-operator-'
        then
            kubectl delete $obj $(kubectl get $obj | grep '^verticadb-operator-' | cut -d' ' -f1) || true
        fi
    done
    set -o xtrace
}

set -o xtrace

if helm list --all-namespaces --filter $HELM_RELEASE_NAME | grep -q $HELM_RELEASE_NAME
then
    while helm list --all-namespaces --filter $HELM_RELEASE_NAME | grep -q $HELM_RELEASE_NAME
    do
        NS=$(helm list --all-namespaces --filter vdb-op --output json | jq -r '[.[].namespace][0]')
        helm uninstall -n $NS $HELM_RELEASE_NAME
        remove_cluster_objects  
    done
elif kubectl get subscription --all-namespaces=true | grep -cqe "verticadb-operator" 2> /dev/null || \
   kubectl get operatorgroups --all-namespaces=true | grep -cqe "verticadb-operator" 2> /dev/null ||
   kubectl get csv --all-namespaces=true | grep -cqe "VerticaDB Operator" 2> /dev/null
then
    $SCRIPT_DIR/undeploy-olm.sh
    remove_cluster_objects
elif kubectl get deployment -n verticadb-operator -l control-plane=verticadb-operator | grep -cqe "verticadb-operator" 2> /dev/null
then
    kubectl delete -f $REPO_DIR/config/release-manifests/operator.yaml || true
    remove_cluster_objects
else
    remove_cluster_objects
    echo "** No operator deployment detected"
    if [ -n "$IGNORE_NOT_FOUND" ]
    then
        exit 0
    fi
    exit 1
fi
