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

# A script that will setup azurite for use with e2e tests

set -o errexit
set -o pipefail

NS=kuttl-e2e-azb
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)
TIMEOUT=360
KUSTOMIZE=$REPO_DIR/bin/kustomize

function usage {
    echo "usage: $0 [-u] [-t <seconds>]"
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
kubectl apply -n $NS -f $REPO_DIR/tests/manifests/azurite/base/azurite-server.yaml
kubectl wait -n $NS --for=condition=Ready=True pod azurite --timeout ${TIMEOUT}s

# Create the azure blob container that we will use throughout the e2e tests
AZURITE_POD_IP=$(kubectl get pods -n $NS azurite -o jsonpath={.status.podIP})
AZURE_ACCOUNT_NAME=devstoreaccount1
AZURE_ACCOUNT_KEY=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==

pushd $REPO_DIR/tests/manifests/azurite > /dev/null
mkdir -p overlay
cat <<EOF > overlay/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- ../base

patches:
- target:
    version: v1
    kind: Pod
    name: create-container
  patch: |-
    - op: replace
      path: /spec/containers/0/env/0/value
      value: "DefaultEndpointsProtocol=http;AccountName=$AZURE_ACCOUNT_NAME;AccountKey=$AZURE_ACCOUNT_KEY;BlobEndpoint=http://$AZURITE_POD_IP:10000/$AZURE_ACCOUNT_NAME;QueueEndpoint=http://$AZURITE_POD_IP:10001/$AZURE_ACCOUNT_NAME;"
EOF

$KUSTOMIZE build overlay | kubectl apply -n $NS -f -
kubectl kuttl assert -n $NS base/assert.yaml --timeout ${TIMEOUT}

popd > /dev/null
