#!/bin/bash

# (c) Copyright [2021-2022] Open Text.
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
NAMESPACE=$(kubectl config view --minify --output 'jsonpath={..namespace}')
HELM_RELEASE_NAME=$(grep 'HELM_RELEASE_NAME?=' $REPO_DIR/Makefile | cut -d'=' -f2)

function usage() {
    echo "usage: $(basename $0) [-n <namespace>] [-e <helm_release_name>] [-i]"
    echo
    echo "Options:"
    echo "  -n <namespace>          Undeploy the operator found in this namespace.  If this is "
    echo "                          omitted it will pick the current namespace as set in the "
    echo "                          kubectl config."
    echo "  -e <helm_release_name>  Name of the helm release to look for and undeploy if present."
    echo "  -i                      Ignore, and don't fail, when deployment isn't present."
    exit 1
}

while getopts "n:hi" opt
do
    case $opt in
        n)
            NAMESPACE=$OPTARG
            ;;
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

if [ -z "$NAMESPACE" ]
then
    NAMESPACE=default
fi

set -o xtrace

if kubectl get -n $NAMESPACE clusterserviceversion | grep -cqe "^verticadb-operator" 2> /dev/null
then
    $SCRIPT_DIR/undeploy-olm.sh -n $NAMESPACE
elif helm list -n $NAMESPACE| grep -cqe "^$HELM_RELEASE_NAME"
then
	helm uninstall -n $NAMESPACE $HELM_RELEASE_NAME
else
    echo "** No operator deployment detected"
    if [ -n "$IGNORE_NOT_FOUND" ]
    then
        exit 0
    fi
    exit 1
fi
