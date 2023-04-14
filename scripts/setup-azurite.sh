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

# A script that will setup azurite for use with e2e tests

set -o errexit
set -o pipefail

NS=kuttl-e2e-azb
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=360
MANIFEST_PATH=$REPO_DIR/tests/manifests/azurite/base

function usage {
    echo "usage: $0 [-t <seconds>]"
    echo
    echo "Options:"
    echo "  -t <seconds>  Length of the timeout."
    echo
    exit 1
}

OPTIND=1
while getopts "ht:" opt; do
    case ${opt} in
        h)
            usage
            ;;
        t)
            TIMEOUT=$OPTARG
            ;;
        \?)
            echo "Unknown option: -${opt}"
            usage
            ;;
    esac
done

set -o xtrace
kubectl delete namespace $NS || :
kubectl create namespace $NS 

# Start the azurite service
kubectl apply -n $NS -f $MANIFEST_PATH/azurite-server.yaml
# Wait for the pod to exist before putting a wait condition on it
while ! kubectl get pod -n $NS azurite-0; do sleep 0.1; done
kubectl wait -n $NS --for=condition=Ready=True pod azurite-0 --timeout ${TIMEOUT}s

# Create the azure blob container that we will use throughout the e2e tests
kubectl apply -n $NS -f $MANIFEST_PATH/create-container.yaml
kubectl kuttl assert -n $NS $MANIFEST_PATH/assert.yaml --timeout ${TIMEOUT}
