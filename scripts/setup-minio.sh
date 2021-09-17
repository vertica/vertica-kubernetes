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

# A script that will setup minio for use with e2e tests

set -o errexit
set -o pipefail
set -o xtrace

MINIO_NS=kuttl-e2e-communal
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
REPO_DIR=$(dirname $SCRIPT_DIR)

kubectl delete namespace $MINIO_NS || :
kubectl create namespace $MINIO_NS

# Create the cert that will be used for https access.  This will create a secret
# with the tls keys.
kubectl apply -f $REPO_DIR/tests/manifests/minio/01-cert.yaml -n $MINIO_NS
kubectl kuttl assert -n $MINIO_NS --timeout 180 $REPO_DIR/tests/manifests/minio/01-assert.yaml

# Make the tls keys be available through kustomize by copying it into the
# communal-cfg.yaml
$REPO_DIR/tests/create-kustomize-overlay.sh

kubectl krew update
kubectl krew install --manifest-url https://raw.githubusercontent.com/kubernetes-sigs/krew-index/9ee1af89f729b999bcd37f90484c4d74c70a1df2/plugins/minio.yaml
# If these images ever change, they must be updated in tests/external-images.txt
kubectl minio init --console-image minio/console:v0.9.8 --image minio/operator:v4.2.7

# The above command will create the CRD.  But there is a timing hole where the
# CRD is not yet registered with k8s, causing the tenant creation below to
# fail.  Add a wait until we know the CRD exists.
set +o xtrace
set +o errexit
echo "Waiting for CRD to be created..."
while [[ $(kubectl api-resources --api-group=minio.min.io -o name | wc -l) = "0" ]]
do
    sleep 0.1
done
set -o errexit
set +o xtrace

kustomize build $REPO_DIR/tests/manifests/s3-creds/base | kubectl apply -f - -n $MINIO_NS
kubectl apply -f $REPO_DIR/tests/manifests/minio/02-tenant.yaml -n $MINIO_NS
kubectl kuttl assert -n $MINIO_NS --timeout 180 $REPO_DIR/tests/manifests/minio/02-assert.yaml
